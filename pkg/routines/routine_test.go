package routines

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
)

func setupTestDB(t *testing.T) *deck.StateDB {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "state.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("failed to open test db: %v", err)
	}

	schema := `
	CREATE TABLE IF NOT EXISTS routines (
		id TEXT PRIMARY KEY,
		title TEXT NOT NULL,
		cron_expr TEXT NOT NULL,
		command TEXT NOT NULL,
		tool TEXT NOT NULL DEFAULT 'agy',
		account TEXT NOT NULL DEFAULT 'default',
		skills TEXT NOT NULL DEFAULT 'core',
		status TEXT NOT NULL DEFAULT 'active',
		last_run_at INTEGER NOT NULL DEFAULT 0,
		next_run_at INTEGER NOT NULL DEFAULT 0,
		last_status TEXT NOT NULL DEFAULT '',
		last_output TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL
	);
	`
	if _, err := db.Exec(schema); err != nil {
		t.Fatalf("failed to create schema: %v", err)
	}
	db.Close()

	sdb, err := deck.OpenStateDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open state db: %v", err)
	}
	return sdb
}

func TestRoutines_Lifecycle(t *testing.T) {
	sdb := setupTestDB(t)
	defer sdb.Close()

	// 1. Create Routine
	rec := RoutineRecord{
		Title:    "Daily Scrape Routine",
		CronExpr: "0 9 * * *",
		Command:  "echo 'running scraper'",
		Tool:     "agy",
		Account:  "tester@example.com",
		Skills:   "core,web",
	}

	created, err := CreateRoutine(sdb, rec)
	if err != nil {
		t.Fatalf("CreateRoutine failed: %v", err)
	}
	if created.ID == "" {
		t.Fatalf("expected generated ID")
	}
	if created.Title != rec.Title {
		t.Errorf("expected title %q, got %q", rec.Title, created.Title)
	}
	if created.NextRunAt <= 0 {
		t.Errorf("expected NextRunAt calculated, got %d", created.NextRunAt)
	}

	// 2. Load Routines
	all, err := LoadRoutines(sdb)
	if err != nil {
		t.Fatalf("LoadRoutines failed: %v", err)
	}
	if len(all) != 1 {
		t.Fatalf("expected 1 routine, got %d", len(all))
	}

	// 3. Find Routine
	found, err := FindRoutine(sdb, created.ID)
	if err != nil {
		t.Fatalf("FindRoutine failed: %v", err)
	}
	if found.Title != created.Title {
		t.Errorf("expected found title %q, got %q", created.Title, found.Title)
	}

	// 4. Update Status (pause / resume)
	if err := UpdateRoutineStatus(sdb, created.ID, "paused"); err != nil {
		t.Fatalf("UpdateRoutineStatus failed: %v", err)
	}
	paused, _ := FindRoutine(sdb, created.ID)
	if paused.Status != "paused" {
		t.Errorf("expected paused status, got %q", paused.Status)
	}

	// 5. Test Scheduler TriggerNow
	scheduler := NewScheduler(sdb, 100*time.Millisecond)
	triggered, err := scheduler.TriggerNow(created.ID)
	if err != nil {
		t.Fatalf("TriggerNow failed: %v", err)
	}
	if triggered.ID != created.ID {
		t.Errorf("expected triggered ID %q, got %q", created.ID, triggered.ID)
	}

	// Wait briefly for execution
	time.Sleep(150 * time.Millisecond)

	// Verify last run outcome recorded
	afterRun, err := FindRoutine(sdb, created.ID)
	if err != nil {
		t.Fatalf("FindRoutine after run failed: %v", err)
	}
	if afterRun.LastRunAt == 0 {
		t.Errorf("expected LastRunAt to be updated")
	}
	if !strings.Contains(afterRun.LastOutput, "running scraper") {
		t.Errorf("expected output to contain 'running scraper', got %q", afterRun.LastOutput)
	}

	// 6. Delete Routine
	if err := DeleteRoutine(sdb, created.ID); err != nil {
		t.Fatalf("DeleteRoutine failed: %v", err)
	}
	afterDel, _ := LoadRoutines(sdb)
	if len(afterDel) != 0 {
		t.Errorf("expected 0 routines after delete, got %d", len(afterDel))
	}
}
