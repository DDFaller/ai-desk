package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVirtualizer_PrepareSandbox(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "shared_skills")
	if err := os.MkdirAll(skillsDir, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(skillsDir, "test_skill.md")
	if err := os.WriteFile(testFile, []byte("# Test Skill"), 0644); err != nil {
		t.Fatal(err)
	}

	harnessesDir := filepath.Join(tempDir, "harnesses")
	v := NewVirtualizer(harnessesDir, skillsDir)

	// Test Claude
	env, err := v.PrepareSandbox("claude", "work-account")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedClaudeDir := filepath.Join(harnessesDir, "claude", "work-account")
	if env["CLAUDE_CONFIG_DIR"] != expectedClaudeDir {
		t.Errorf("got %s, want %s", env["CLAUDE_CONFIG_DIR"], expectedClaudeDir)
	}
	// Verify skills symlink exists
	linkedSkills := filepath.Join(expectedClaudeDir, "skills")
	if info, err := os.Lstat(linkedSkills); err != nil || (info.Mode()&os.ModeSymlink == 0) {
		t.Errorf("expected symlink at %s, got err: %v", linkedSkills, err)
	}

	// Test Antigravity / Agy
	envAgy, err := v.PrepareSandbox("agy", "personal-account")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedAgyDir := filepath.Join(harnessesDir, "agy", "personal-account")
	if envAgy["GEMINI_CLI_HOME"] != expectedAgyDir {
		t.Errorf("got %s, want %s", envAgy["GEMINI_CLI_HOME"], expectedAgyDir)
	}

	// Test token write
	if err := v.WriteAgyToken("personal-account", `{"access_token":"secret"}`, "user@example.com"); err != nil {
		t.Fatalf("write token failed: %v", err)
	}
	tokenFile := filepath.Join(expectedAgyDir, "oauth_tokens.json")
	if _, err := os.Stat(tokenFile); err != nil {
		t.Errorf("expected token file to exist at %s", tokenFile)
	}
}
