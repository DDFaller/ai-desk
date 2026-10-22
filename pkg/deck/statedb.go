package deck

import (
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// GroupRecord mirrors a row from agent-deck's groups table.
type GroupRecord struct {
	Path          string
	Name          string
	Expanded      bool
	SortOrder     int
	DefaultPath   string
	MaxConcurrent int
}

// InstanceRecord mirrors a row from agent-deck's instances table.
type InstanceRecord struct {
	ID          string
	Title       string
	ProjectPath string
	GroupPath   string
	SortOrder   int
	Command     string
	Wrapper     string
	Tool        string
	Status      string
	TmuxSession string
	CreatedAt   int64
	LastAccessed int64
	Account     string
	ToolData    string
}

// TokenUsage holds token metrics aggregated from transcript / cost events.
type TokenUsage struct {
	TotalTokens        int64
	InputTokens        int64
	OutputTokens       int64
	CachedInputTokens  int64
	ReasoningTokens    int64
	ModelContextWindow int64
}

// StateDB coordinates directly with Agent-Deck's state.db.
type StateDB struct {
	db   *sql.DB
	path string
}

// DefaultStateDBPath returns the standard agent-deck state.db location.
func DefaultStateDBPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "agent-deck", "profiles", "default", "state.db")
}

// OpenStateDB opens connection to agent-deck's SQLite database.
func OpenStateDB(dbPath string) (*StateDB, error) {
	if dbPath == "" {
		dbPath = DefaultStateDBPath()
	}

	if _, err := os.Stat(dbPath); err != nil {
		return nil, fmt.Errorf("state.db not found at %s: %w", dbPath, err)
	}

	// Open with WAL mode compatible pragma
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}

	return &StateDB{db: db, path: dbPath}, nil
}

// Close closes the database connection.
func (s *StateDB) Close() error {
	if s.db != nil {
		return s.db.Close()
	}
	return nil
}

// LoadGroups retrieves all group definitions ordered by sort_order and path.
func (s *StateDB) LoadGroups() ([]GroupRecord, error) {
	rows, err := s.db.Query("SELECT path, name, expanded, sort_order, default_path, max_concurrent FROM groups ORDER BY sort_order, path")
	if err != nil {
		return nil, fmt.Errorf("query groups: %w", err)
	}
	defer rows.Close()

	var groups []GroupRecord
	for rows.Next() {
		var g GroupRecord
		var exp int
		if err := rows.Scan(&g.Path, &g.Name, &exp, &g.SortOrder, &g.DefaultPath, &g.MaxConcurrent); err != nil {
			return nil, err
		}
		g.Expanded = exp == 1
		groups = append(groups, g)
	}
	return groups, nil
}

// LoadInstances retrieves all instance records.
func (s *StateDB) LoadInstances() ([]InstanceRecord, error) {
	query := `SELECT id, title, project_path, group_path, sort_order, command, wrapper, tool, status, tmux_session, created_at, last_accessed, account, tool_data FROM instances ORDER BY sort_order, title`
	rows, err := s.db.Query(query)
	if err != nil {
		return nil, fmt.Errorf("query instances: %w", err)
	}
	defer rows.Close()

	var instances []InstanceRecord
	for rows.Next() {
		var inst InstanceRecord
		if err := rows.Scan(&inst.ID, &inst.Title, &inst.ProjectPath, &inst.GroupPath, &inst.SortOrder, &inst.Command, &inst.Wrapper, &inst.Tool, &inst.Status, &inst.TmuxSession, &inst.CreatedAt, &inst.LastAccessed, &inst.Account, &inst.ToolData); err != nil {
			return nil, err
		}
		instances = append(instances, inst)
	}
	return instances, nil
}

// CreateGroup creates a new group record, creating any intermediate parents if missing.
func (s *StateDB) CreateGroup(groupPath string) error {
	trimmed := strings.Trim(groupPath, "/")
	if trimmed == "" {
		return fmt.Errorf("invalid empty group path")
	}

	segments := strings.Split(trimmed, "/")
	currentPath := ""

	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for _, seg := range segments {
		if currentPath == "" {
			currentPath = seg
		} else {
			currentPath = currentPath + "/" + seg
		}

		// Insert parent/group if it doesn't already exist
		_, err := tx.Exec(`INSERT OR IGNORE INTO groups (path, name, expanded, sort_order, default_path, max_concurrent)
			VALUES (?, ?, 1, 0, '', 0)`, currentPath, seg)
		if err != nil {
			return fmt.Errorf("insert group %s: %w", currentPath, err)
		}
	}

	return tx.Commit()
}

// MoveSession moves an instance to a target group path.
func (s *StateDB) MoveSession(sessionIDOrTitle, targetGroup string) error {
	// First ensure target group exists
	if err := s.CreateGroup(targetGroup); err != nil {
		return err
	}

	res, err := s.db.Exec(`UPDATE instances SET group_path = ? WHERE id = ? OR title = ?`, targetGroup, sessionIDOrTitle, sessionIDOrTitle)
	if err != nil {
		return fmt.Errorf("update instance group: %w", err)
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("session %q not found", sessionIDOrTitle)
	}
	return nil
}

// DeleteGroupCascade terminates all tmux processes in group and subgroups,
// and removes all sessions and group records in an atomic transaction.
func (s *StateDB) DeleteGroupCascade(groupPath string) ([]InstanceRecord, error) {
	trimmed := strings.Trim(groupPath, "/")
	if trimmed == "" {
		return nil, fmt.Errorf("cannot delete root")
	}

	// 1. Identify all instances to be deleted
	query := `SELECT id, title, project_path, group_path, sort_order, command, wrapper, tool, status, tmux_session, created_at, last_accessed, account, tool_data 
		FROM instances 
		WHERE group_path = ? OR group_path LIKE ? || '/%'`
	rows, err := s.db.Query(query, trimmed, trimmed)
	if err != nil {
		return nil, fmt.Errorf("find cascade instances: %w", err)
	}
	defer rows.Close()

	var targets []InstanceRecord
	for rows.Next() {
		var inst InstanceRecord
		if err := rows.Scan(&inst.ID, &inst.Title, &inst.ProjectPath, &inst.GroupPath, &inst.SortOrder, &inst.Command, &inst.Wrapper, &inst.Tool, &inst.Status, &inst.TmuxSession, &inst.CreatedAt, &inst.LastAccessed, &inst.Account, &inst.ToolData); err != nil {
			return nil, err
		}
		targets = append(targets, inst)
	}
	rows.Close()

	// 2. Kill tmux sessions for all targeted instances
	for _, inst := range targets {
		if inst.TmuxSession != "" {
			_ = exec.Command("tmux", "kill-session", "-t", inst.TmuxSession).Run()
		}
	}

	// 3. Atomically delete from database
	tx, err := s.db.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	// Delete instances
	_, err = tx.Exec(`DELETE FROM instances WHERE group_path = ? OR group_path LIKE ? || '/%'`, trimmed, trimmed)
	if err != nil {
		return nil, fmt.Errorf("delete instances: %w", err)
	}

	// Delete groups
	_, err = tx.Exec(`DELETE FROM groups WHERE path = ? OR path LIKE ? || '/%'`, trimmed, trimmed)
	if err != nil {
		return nil, fmt.Errorf("delete groups: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return nil, err
	}

	return targets, nil
}
