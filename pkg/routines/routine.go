package routines

import (
	"crypto/md5"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
)

// RoutineRecord represents a scheduled recurring agent task or cron job.
type RoutineRecord struct {
	ID         string `json:"id"`
	Title      string `json:"title"`
	CronExpr   string `json:"cron_expr"`
	Command    string `json:"command"`
	Tool       string `json:"tool"`
	Account    string `json:"account"`
	Skills     string `json:"skills"`
	Status     string `json:"status"` // "active", "paused"
	LastRunAt  int64  `json:"last_run_at"`
	NextRunAt  int64  `json:"next_run_at"`
	LastStatus string `json:"last_status"`
	LastOutput string `json:"last_output"`
	CreatedAt  int64  `json:"created_at"`
}

// CreateRoutine registers a new scheduled routine in state.db.
func CreateRoutine(sdb *deck.StateDB, r RoutineRecord) (*RoutineRecord, error) {
	if sdb == nil || sdb.DB() == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	title := strings.TrimSpace(r.Title)
	if title == "" {
		return nil, fmt.Errorf("routine title cannot be empty")
	}

	cronExpr := strings.TrimSpace(r.CronExpr)
	if cronExpr == "" {
		cronExpr = "0 9 * * *" // default daily at 9am
	}

	sched, err := ParseCron(cronExpr, time.Local)
	if err != nil {
		return nil, fmt.Errorf("invalid cron expression %q: %w", cronExpr, err)
	}

	tool := strings.TrimSpace(r.Tool)
	if tool == "" {
		tool = "agy"
	}

	account := strings.TrimSpace(r.Account)
	if account == "" {
		account = "default"
	}

	skills := strings.TrimSpace(r.Skills)
	if skills == "" {
		skills = "core"
	}

	status := strings.TrimSpace(r.Status)
	if status == "" {
		status = "active"
	}

	id := r.ID
	if id == "" {
		hash := md5.Sum([]byte(fmt.Sprintf("%s-%s-%d", title, cronExpr, time.Now().UnixNano())))
		id = fmt.Sprintf("rt-%x-%d", hash[:4], time.Now().Unix()%10000)
	}

	now := time.Now()
	nextRun, ok := sched.Next(now)
	nextRunEpoch := int64(0)
	if ok {
		nextRunEpoch = nextRun.Unix()
	}

	query := `INSERT INTO routines (
		id, title, cron_expr, command, tool, account, skills,
		status, last_run_at, next_run_at, last_status, last_output, created_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, 0, ?, '', '', ?)`

	_, err = sdb.DB().Exec(query,
		id, title, cronExpr, r.Command, tool, account, skills,
		status, nextRunEpoch, now.Unix(),
	)
	if err != nil {
		return nil, fmt.Errorf("insert routine: %w", err)
	}

	r.ID = id
	r.Title = title
	r.CronExpr = cronExpr
	r.Tool = tool
	r.Account = account
	r.Skills = skills
	r.Status = status
	r.NextRunAt = nextRunEpoch
	r.CreatedAt = now.Unix()

	return &r, nil
}

// LoadRoutines retrieves all registered routines.
func LoadRoutines(sdb *deck.StateDB) ([]RoutineRecord, error) {
	if sdb == nil || sdb.DB() == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	query := `SELECT id, title, cron_expr, command, tool, account, skills, status, last_run_at, next_run_at, last_status, last_output, created_at 
		FROM routines ORDER BY created_at DESC`
	rows, err := sdb.DB().Query(query)
	if err != nil {
		return nil, fmt.Errorf("query routines: %w", err)
	}
	defer rows.Close()

	var results []RoutineRecord
	for rows.Next() {
		var r RoutineRecord
		if err := rows.Scan(
			&r.ID, &r.Title, &r.CronExpr, &r.Command, &r.Tool, &r.Account, &r.Skills,
			&r.Status, &r.LastRunAt, &r.NextRunAt, &r.LastStatus, &r.LastOutput, &r.CreatedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, r)
	}
	return results, nil
}

// FindRoutine locates a single routine by ID or title.
func FindRoutine(sdb *deck.StateDB, identifier string) (*RoutineRecord, error) {
	if sdb == nil || sdb.DB() == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	query := `SELECT id, title, cron_expr, command, tool, account, skills, status, last_run_at, next_run_at, last_status, last_output, created_at 
		FROM routines WHERE id = ? OR title = ? LIMIT 1`
	var r RoutineRecord
	err := sdb.DB().QueryRow(query, identifier, identifier).Scan(
		&r.ID, &r.Title, &r.CronExpr, &r.Command, &r.Tool, &r.Account, &r.Skills,
		&r.Status, &r.LastRunAt, &r.NextRunAt, &r.LastStatus, &r.LastOutput, &r.CreatedAt,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("routine %q not found", identifier)
		}
		return nil, err
	}
	return &r, nil
}

// UpdateRoutineStatus sets a routine's status (active or paused).
func UpdateRoutineStatus(sdb *deck.StateDB, id, status string) error {
	if sdb == nil || sdb.DB() == nil {
		return fmt.Errorf("state.db connection is nil")
	}

	res, err := sdb.DB().Exec(`UPDATE routines SET status = ? WHERE id = ?`, status, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("routine with id %q not found", id)
	}
	return nil
}

// DeleteRoutine removes a routine definition from state.db.
func DeleteRoutine(sdb *deck.StateDB, id string) error {
	if sdb == nil || sdb.DB() == nil {
		return fmt.Errorf("state.db connection is nil")
	}

	res, err := sdb.DB().Exec(`DELETE FROM routines WHERE id = ?`, id)
	if err != nil {
		return err
	}
	rows, _ := res.RowsAffected()
	if rows == 0 {
		return fmt.Errorf("routine with id %q not found", id)
	}
	return nil
}

// RecordRoutineRun updates a routine after it has executed.
func RecordRoutineRun(sdb *deck.StateDB, id, lastStatus, output string, nextRun int64) error {
	if sdb == nil || sdb.DB() == nil {
		return fmt.Errorf("state.db connection is nil")
	}

	now := time.Now().Unix()
	query := `UPDATE routines SET last_run_at = ?, next_run_at = ?, last_status = ?, last_output = ? WHERE id = ?`
	_, err := sdb.DB().Exec(query, now, nextRun, lastStatus, output, id)
	return err
}
