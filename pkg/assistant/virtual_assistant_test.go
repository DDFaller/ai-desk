package assistant

import (
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	_ "modernc.org/sqlite"
)

func setupTestDB(t *testing.T) *deck.StateDB {
	t.Helper()
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	createSchema := `
	CREATE TABLE groups (
		path TEXT PRIMARY KEY,
		name TEXT,
		expanded INTEGER,
		sort_order INTEGER,
		default_path TEXT,
		max_concurrent INTEGER
	);
	CREATE TABLE instances (
		id TEXT PRIMARY KEY,
		title TEXT,
		project_path TEXT,
		group_path TEXT,
		sort_order INTEGER,
		command TEXT,
		wrapper TEXT,
		tool TEXT,
		status TEXT,
		tmux_session TEXT,
		created_at INTEGER,
		last_accessed INTEGER,
		account TEXT,
		tool_data TEXT
	);
	`
	if _, err := db.Exec(createSchema); err != nil {
		t.Fatalf("create test schema: %v", err)
	}

	_, err = db.Exec(`INSERT INTO instances (id, title, project_path, group_path, sort_order, command, wrapper, tool, status, tmux_session, created_at, last_accessed, account, tool_data)
		VALUES ('task-1', 'moraaky-ai', '/tmp', 'core', 0, 'agy', '', 'agy', 'running', '', 1000, 2000, 'danielmcf15@gmail.com', '{}')`)
	if err != nil {
		t.Fatalf("insert test instance: %v", err)
	}
	db.Close()

	sdb, err := deck.OpenStateDB(dbPath)
	if err != nil {
		t.Fatalf("OpenStateDB failed: %v", err)
	}
	return sdb
}

func TestVirtualAssistant_MonitorAndBriefing(t *testing.T) {
	sdb := setupTestDB(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")
	cfg := DefaultAssistantConfig()
	cfg.VoiceEnabled = false // disable audio in unit test

	assistant := NewVirtualAssistant(sdb, virt, cfg)

	// 1. Run monitor tick
	alerts, err := assistant.MonitorTick()
	if err != nil {
		t.Fatalf("MonitorTick failed: %v", err)
	}
	t.Logf("Alerts on initial tick: %v", alerts)

	// 2. Generate Briefing
	briefing := assistant.GenerateBriefing()
	if !strings.Contains(briefing, "Resident Virtual Assistant") {
		t.Errorf("expected briefing header: %s", briefing)
	}
	if !strings.Contains(briefing, "Active Workloads: 1 sessions") {
		t.Errorf("expected 1 session reported in briefing: %s", briefing)
	}
}
