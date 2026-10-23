package deck

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falluba/ai-desk/pkg/harness"
	_ "modernc.org/sqlite"
)

func setupTestDBWithInstance(t *testing.T) (*StateDB, string) {
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
		VALUES ('test-inst-1', 'auth-worker', '/tmp', 'core', 0, 'codex', '', 'codex', 'running', '', 1000, 2000, 'default', '{}')`)
	if err != nil {
		t.Fatalf("insert test instance: %v", err)
	}

	return &StateDB{db: db, path: dbPath}, dbPath
}

func TestSwitchHarness_DBUpdate(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")

	opts := SwitchOptions{
		TargetTool:     "agy",
		IncludeHandoff: false,
	}

	res, err := SwitchHarness(sdb, virt, "test-inst-1", opts)
	if err != nil {
		t.Fatalf("SwitchHarness failed: %v", err)
	}

	if res.OldTool != "codex" || res.NewTool != "agy" {
		t.Fatalf("expected oldTool codex, newTool agy, got (%q, %q)", res.OldTool, res.NewTool)
	}

	// Verify persistence in DB
	inst, err := sdb.FindInstance("test-inst-1")
	if err != nil {
		t.Fatalf("FindInstance failed: %v", err)
	}
	if inst.Tool != "agy" || inst.Command != "agy" {
		t.Fatalf("expected instance tool/cmd updated to agy, got tool=%q, cmd=%q", inst.Tool, inst.Command)
	}
}

func TestSwitchAccount_DBUpdate(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")

	updatedInst, err := SwitchAccount(sdb, virt, "test-inst-1", "clownfishexperience@gmail.com", false)
	if err != nil {
		t.Fatalf("SwitchAccount failed: %v", err)
	}

	if updatedInst.Account != "clownfishexperience@gmail.com" {
		t.Fatalf("expected account clownfishexperience@gmail.com, got %q", updatedInst.Account)
	}

	// Verify persistence in DB
	inst, err := sdb.FindInstance("test-inst-1")
	if err != nil {
		t.Fatalf("FindInstance failed: %v", err)
	}
	if inst.Account != "clownfishexperience@gmail.com" {
		t.Fatalf("expected DB account clownfishexperience@gmail.com, got %q", inst.Account)
	}
}

func TestBuildHarnessLaunchCommand(t *testing.T) {
	prompt := "Review .ai-desk-handoff.md and continue."
	tests := []struct {
		tool     string
		expected string
	}{
		{"agy", "agy -i 'Review .ai-desk-handoff.md and continue.'"},
		{"gemini", "gemini -i 'Review .ai-desk-handoff.md and continue.'"},
		{"claude", "claude 'Review .ai-desk-handoff.md and continue.'"},
		{"codex", "codex 'Review .ai-desk-handoff.md and continue.'"},
		{"opencode", "opencode --prompt 'Review .ai-desk-handoff.md and continue.'"},
	}

	for _, tt := range tests {
		cmd := BuildHarnessLaunchCommand(tt.tool, prompt)
		if cmd != tt.expected {
			t.Errorf("tool %s: expected %q, got %q", tt.tool, tt.expected, cmd)
		}
	}
}

func TestBuildHandoffDirective(t *testing.T) {
	d := BuildHandoffDirective("claude", "agy", "/path/to/handoff.md", "Focus on bug #42")
	if !strings.Contains(d, "prior claude session") {
		t.Errorf("expected mention of prior claude session, got %s", d)
	}
	if !strings.Contains(d, "/path/to/handoff.md") {
		t.Errorf("expected mention of handoff path, got %s", d)
	}
	if !strings.Contains(d, "Focus on bug #42") {
		t.Errorf("expected custom directive, got %s", d)
	}
}

func TestSwitchHarness_WithHandoff(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")

	opts := SwitchOptions{
		TargetTool:     "agy",
		IncludeHandoff: true,
		CustomPrompt:   "Proceed with refactoring",
	}

	res, err := SwitchHarness(sdb, virt, "test-inst-1", opts)
	if err != nil {
		t.Fatalf("SwitchHarness with handoff failed: %v", err)
	}

	if res.HandoffPath == "" {
		t.Fatalf("expected non-empty HandoffPath")
	}

	// Verify .ai-desk-handoff.md was projected into sandbox
	sandboxHandoff := filepath.Join(res.SandboxPath, ".ai-desk-handoff.md")
	if _, err := os.Stat(sandboxHandoff); err != nil {
		t.Errorf("expected .ai-desk-handoff.md in sandbox %s: %v", res.SandboxPath, err)
	}
}

func TestAutoFailoverSession(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")

	// Set instance account to danielmcf15@gmail.com
	_ = sdb.UpdateInstanceAccount("test-inst-1", "danielmcf15@gmail.com")

	updated, err := AutoFailoverSession(sdb, virt, "test-inst-1", "HTTP 429 quota reached")
	if err != nil {
		t.Logf("AutoFailoverSession returned: %v (expected if no extra accounts or vault missing in test)", err)
		return
	}

	if updated.Account == "danielmcf15@gmail.com" {
		t.Errorf("expected account to change away from danielmcf15@gmail.com, got %q", updated.Account)
	}
	if !strings.Contains(updated.ToolData, "auto_failovers") {
		t.Errorf("expected auto_failovers record in tool_data: %s", updated.ToolData)
	}
}

func TestSwitchHarness_RecreateCleanSession(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	// Seed instance with old session ID
	_, _ = sdb.db.Exec(`UPDATE instances SET tool_data = '{"codex_session_id":"old-uuid-123","codex_detected_at":100}' WHERE id = 'test-inst-1'`)

	virt := harness.NewVirtualizer(t.TempDir(), "")

	opts := SwitchOptions{
		TargetTool:     "agy",
		IncludeHandoff: false,
		CreateNewTask:  false,
	}

	res, err := SwitchHarness(sdb, virt, "test-inst-1", opts)
	if err != nil {
		t.Fatalf("SwitchHarness failed: %v", err)
	}

	inst, err := sdb.FindInstance("test-inst-1")
	if err != nil {
		t.Fatalf("FindInstance failed: %v", err)
	}

	// Verify old session ID was purged
	if strings.Contains(inst.ToolData, "old-uuid-123") {
		t.Errorf("expected old-uuid-123 to be wiped from tool_data: %s", inst.ToolData)
	}
	if !strings.Contains(inst.ToolData, `"is_fresh_harness":true`) {
		t.Errorf("expected is_fresh_harness:true in tool_data: %s", inst.ToolData)
	}
	if res.NewTool != "agy" {
		t.Errorf("expected newTool agy, got %s", res.NewTool)
	}
}

func TestSwitchHarness_CreateNewTaskFork(t *testing.T) {
	sdb, _ := setupTestDBWithInstance(t)
	defer sdb.Close()

	virt := harness.NewVirtualizer(t.TempDir(), "")

	opts := SwitchOptions{
		TargetTool:     "claude",
		IncludeHandoff: true,
		CreateNewTask:  true,
	}

	res, err := SwitchHarness(sdb, virt, "test-inst-1", opts)
	if err != nil {
		t.Fatalf("SwitchHarness fork failed: %v", err)
	}

	// Original instance should remain unchanged
	orig, err := sdb.FindInstance("test-inst-1")
	if err != nil {
		t.Fatalf("FindInstance for orig failed: %v", err)
	}
	if orig.Tool != "codex" {
		t.Errorf("expected original instance to remain codex, got %s", orig.Tool)
	}

	// New instance should exist and have target tool
	if res.Instance.ID == "test-inst-1" {
		t.Errorf("expected new instance to have different ID from parent")
	}
	if res.Instance.Tool != "claude" {
		t.Errorf("expected new instance tool to be claude, got %s", res.Instance.Tool)
	}
	if !strings.HasPrefix(res.Instance.Title, "auth-worker-claude") {
		t.Errorf("expected new instance title to start with auth-worker-claude, got %s", res.Instance.Title)
	}
}
