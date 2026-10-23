package deck

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestBuildHandoffBrief(t *testing.T) {
	brief := &HandoffBrief{
		SessionID:    "test-sess-1",
		Title:        "Feature Auth",
		ProjectPath:  "/home/user/projects/auth",
		SourceTool:   "codex",
		TargetTool:   "agy",
		GitBranch:    "feat/jwt (a1b2c3d)",
		GitStatus:    " M auth/handler.go\n?? auth/token.go",
		GitDiffStat:  "auth/handler.go | 12 +++++++++++-\n 1 file changed, 11 insertions(+), 1 deletion(-)",
		TerminalTail: "User: Please implement the JWT refresh logic.\nCodex: Working on auth/handler.go...",
		CustomPrompt: "Focus on verifying test coverage.",
		Timestamp:    time.Now(),
	}

	content := BuildHandoffBrief(brief)

	if !strings.Contains(content, "=== CROSS-HARNESS SESSION HANDOFF ===") {
		t.Fatalf("expected header in handoff brief, got:\n%s", content)
	}
	if !strings.Contains(content, "Feature Auth") || !strings.Contains(content, "test-sess-1") {
		t.Fatalf("expected title and session ID in handoff brief")
	}
	if !strings.Contains(content, "codex") || !strings.Contains(content, "agy") {
		t.Fatalf("expected tools in handoff brief")
	}
	if !strings.Contains(content, "feat/jwt") {
		t.Fatalf("expected git branch in handoff brief")
	}
	if !strings.Contains(content, "auth/token.go") {
		t.Fatalf("expected git status in handoff brief")
	}
	if !strings.Contains(content, "Focus on verifying test coverage.") {
		t.Fatalf("expected custom prompt in handoff brief")
	}
	if !strings.Contains(content, "HANDOFF RECEIVED") {
		t.Fatalf("expected handoff receipt prompt in brief")
	}
}

func TestSaveHandoffBrief(t *testing.T) {
	tmpDir := t.TempDir()
	content := "Test Handoff Content"

	path, err := SaveHandoffBrief(tmpDir, "sess-xyz", content)
	if err != nil {
		t.Fatalf("SaveHandoffBrief failed: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected saved file at %s, got: %v", path, err)
	}

	// Verify project path copy
	projCopy := filepath.Join(tmpDir, ".ai-desk-handoff.md")
	data, err := os.ReadFile(projCopy)
	if err != nil {
		t.Fatalf("expected project copy at %s: %v", projCopy, err)
	}
	if string(data) != content {
		t.Fatalf("expected %q, got %q", content, string(data))
	}
}

func TestNormalizeHarnessName(t *testing.T) {
	tests := []struct {
		input       string
		wantTool    string
		wantCommand string
		wantErr     bool
	}{
		{"agy", "agy", "agy", false},
		{"gemini", "agy", "agy", false},
		{"antigravity", "agy", "agy", false},
		{"claude", "claude", "claude", false},
		{"claude-code", "claude", "claude", false},
		{"codex", "codex", "codex", false},
		{"opencode", "opencode", "opencode", false},
		{"invalid", "", "", true},
	}

	for _, tt := range tests {
		tool, cmd, err := NormalizeHarnessName(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeHarnessName(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if tool != tt.wantTool || cmd != tt.wantCommand {
			t.Errorf("NormalizeHarnessName(%q) = (%q, %q), want (%q, %q)", tt.input, tool, cmd, tt.wantTool, tt.wantCommand)
		}
	}
}
