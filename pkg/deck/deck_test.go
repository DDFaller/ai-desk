package deck

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func setupTestDB(t *testing.T) (*StateDB, string) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "test_state.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	// Create tables matching agent-deck schema
	schema := `
	CREATE TABLE groups (
		path TEXT PRIMARY KEY,
		name TEXT NOT NULL,
		expanded INTEGER NOT NULL DEFAULT 1,
		sort_order INTEGER NOT NULL DEFAULT 0,
		default_path TEXT NOT NULL DEFAULT '',
		max_concurrent INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE instances (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		project_path TEXT NOT NULL,
		group_path TEXT NOT NULL DEFAULT 'my-sessions',
		sort_order INTEGER NOT NULL DEFAULT 0,
		command TEXT NOT NULL DEFAULT '',
		wrapper TEXT NOT NULL DEFAULT '',
		tool TEXT NOT NULL DEFAULT 'shell',
		status TEXT NOT NULL DEFAULT 'idle',
		tmux_session TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL DEFAULT 0,
		last_accessed INTEGER NOT NULL DEFAULT 0,
		account TEXT NOT NULL DEFAULT '',
		tool_data TEXT NOT NULL DEFAULT '{}'
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create tables: %v", err)
	}
	db.Close()

	sdb, err := OpenStateDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open state db: %v", err)
	}
	return sdb, dbPath
}

func TestDeck_GroupsAndCascadeDeletion(t *testing.T) {
	sdb, _ := setupTestDB(t)
	defer sdb.Close()

	// 1. Create nested groups
	if err := sdb.CreateGroup("Work/Backend/Auth"); err != nil {
		t.Fatalf("create group failed: %v", err)
	}
	if err := sdb.CreateGroup("Personal/Projects"); err != nil {
		t.Fatalf("create group failed: %v", err)
	}

	groups, err := sdb.LoadGroups()
	if err != nil {
		t.Fatalf("load groups failed: %v", err)
	}
	// "Work/Backend/Auth" should have created "Work", "Work/Backend", and "Work/Backend/Auth"
	// "Personal/Projects" should have created "Personal", "Personal/Projects"
	if len(groups) != 5 {
		t.Errorf("expected 5 groups with ancestry, got %d", len(groups))
	}

	// 2. Insert test instances
	_, err = sdb.db.Exec(`
		INSERT INTO instances (id, title, project_path, group_path, tool, status, tmux_session) VALUES
		('inst-1', 'Auth Service', '/work/auth', 'Work/Backend/Auth', 'claude', 'idle', 'tmux_auth'),
		('inst-2', 'Backend API', '/work/api', 'Work/Backend', 'codex', 'running', 'tmux_api'),
		('inst-3', 'Frontend Web', '/work/web', 'Work', 'claude', 'idle', 'tmux_web'),
		('inst-4', 'Personal Blog', '/pers/blog', 'Personal/Projects', 'agy', 'idle', 'tmux_blog');
	`)
	if err != nil {
		t.Fatalf("insert instances failed: %v", err)
	}

	instances, err := sdb.LoadInstances()
	if err != nil {
		t.Fatalf("load instances failed: %v", err)
	}
	if len(instances) != 4 {
		t.Fatalf("expected 4 instances, got %d", len(instances))
	}

	// 3. Build hierarchical tree
	roots := BuildGroupTree(groups, instances)
	if len(roots) != 2 { // Work, Personal
		t.Fatalf("expected 2 root groups, got %d", len(roots))
	}

	var workRoot *GroupNode
	for _, r := range roots {
		if r.Name == "Work" {
			workRoot = r
			break
		}
	}
	if workRoot == nil {
		t.Fatal("Work root not found")
	}
	if workRoot.TotalTasks != 3 {
		t.Errorf("expected 3 tasks in Work tree, got %d", workRoot.TotalTasks)
	}

	// Test flattening
	items := FlattenVisibleTree(roots)
	if len(items) == 0 {
		t.Fatal("expected non-empty flattened tree")
	}

	// 4. Test MoveSession
	if err := sdb.MoveSession("inst-4", "Work/Backend"); err != nil {
		t.Fatalf("move session failed: %v", err)
	}
	var updatedGroup string
	_ = sdb.db.QueryRow("SELECT group_path FROM instances WHERE id = 'inst-4'").Scan(&updatedGroup)
	if updatedGroup != "Work/Backend" {
		t.Errorf("expected moved group Work/Backend, got %s", updatedGroup)
	}

	// 5. Test Cascade Deletion of "Work/Backend" (should delete "Work/Backend", "Work/Backend/Auth", inst-1, inst-2, and inst-4)
	deleted, err := sdb.DeleteGroupCascade("Work/Backend")
	if err != nil {
		t.Fatalf("cascade deletion failed: %v", err)
	}
	if len(deleted) != 3 {
		t.Errorf("expected 3 deleted instances, got %d", len(deleted))
	}

	// Verify database state after cascade delete
	var remainingInstances int
	_ = sdb.db.QueryRow("SELECT count(*) FROM instances").Scan(&remainingInstances)
	if remainingInstances != 1 { // Only Frontend Web in 'Work' should remain
		t.Errorf("expected 1 remaining instance, got %d", remainingInstances)
	}

	var remainingGroups int
	_ = sdb.db.QueryRow("SELECT count(*) FROM groups").Scan(&remainingGroups)
	// Work and Personal and Personal/Projects should remain (3 total)
	if remainingGroups != 3 {
		t.Errorf("expected 3 remaining groups, got %d", remainingGroups)
	}
}
