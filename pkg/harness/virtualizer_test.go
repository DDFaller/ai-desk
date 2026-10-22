package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVirtualizer_PrepareSandbox(t *testing.T) {
	tempDir := t.TempDir()
	skillsDir := filepath.Join(tempDir, "shared_skills")
	testSkill := filepath.Join(skillsDir, "test-skill")
	if err := os.MkdirAll(testSkill, 0755); err != nil {
		t.Fatal(err)
	}
	testFile := filepath.Join(testSkill, "SKILL.md")
	if err := os.WriteFile(testFile, []byte("# Test Skill"), 0644); err != nil {
		t.Fatal(err)
	}

	harnessesDir := filepath.Join(tempDir, "harnesses")
	v := NewVirtualizer(harnessesDir, skillsDir)

	// Test Claude with selective skills
	env, err := v.PrepareSandboxWithSkills("claude", "work-account", []string{"test-skill"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	expectedClaudeDir := filepath.Join(harnessesDir, "claude", "work-account")
	if env["CLAUDE_CONFIG_DIR"] != expectedClaudeDir {
		t.Errorf("got %s, want %s", env["CLAUDE_CONFIG_DIR"], expectedClaudeDir)
	}
	linkedSkill := filepath.Join(expectedClaudeDir, "skills", "test-skill")
	if info, err := os.Lstat(linkedSkill); err != nil || (info.Mode()&os.ModeSymlink == 0) {
		t.Errorf("expected symlink at %s, got err: %v", linkedSkill, err)
	}

	// Test Antigravity / Agy
	envAgy, err := v.PrepareSandboxWithSkills("agy", "personal-account", []string{"test-skill"})
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

	// Verify RTK projection for Claude
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".claude", "RTK.md")); err == nil {
		projectedRTK := filepath.Join(expectedClaudeDir, "RTK.md")
		if _, err := os.Lstat(projectedRTK); err != nil {
			t.Errorf("expected RTK.md projected in claude sandbox: %v", err)
		}
	}
}
