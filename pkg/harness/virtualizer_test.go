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
	if env["HOME"] != expectedClaudeDir {
		t.Errorf("got HOME=%s, want %s", env["HOME"], expectedClaudeDir)
	}
	linkedSkill := filepath.Join(expectedClaudeDir, "skills", "test-skill")
	if info, err := os.Lstat(linkedSkill); err != nil || (info.Mode()&os.ModeSymlink == 0) {
		t.Errorf("expected symlink at %s, got err: %v", linkedSkill, err)
	}
	claudeAgentsSkills := filepath.Join(expectedClaudeDir, ".agents", "skills")
	if info, err := os.Lstat(claudeAgentsSkills); err != nil || (info.Mode()&os.ModeSymlink == 0) {
		t.Errorf("expected .agents/skills symlink at %s, got err: %v", claudeAgentsSkills, err)
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
	if envAgy["HOME"] != expectedAgyDir {
		t.Errorf("got HOME=%s, want %s", envAgy["HOME"], expectedAgyDir)
	}
	if envAgy["AI_DESK_ACCOUNT"] != "personal-account" {
		t.Errorf("got AI_DESK_ACCOUNT=%s, want personal-account", envAgy["AI_DESK_ACCOUNT"])
	}

	// Test token write and .gemini tree creation
	sampleToken := `{"token":{"access_token":"ya29.mock","refresh_token":"1//mock","token_type":"Bearer"}}`
	if err := v.WriteAgyToken("personal-account", sampleToken, "user@example.com"); err != nil {
		t.Fatalf("write token failed: %v", err)
	}
	tokenFile := filepath.Join(expectedAgyDir, ".gemini", "antigravity-cli", "antigravity-oauth-token")
	if _, err := os.Stat(tokenFile); err != nil {
		t.Errorf("expected antigravity-oauth-token at %s: %v", tokenFile, err)
	}
	jetskiFile := filepath.Join(expectedAgyDir, ".gemini", "jetski-standalone-oauth-token")
	if _, err := os.Stat(jetskiFile); err != nil {
		t.Errorf("expected jetski-standalone-oauth-token at %s: %v", jetskiFile, err)
	}
	credsFile := filepath.Join(expectedAgyDir, ".gemini", "oauth_creds.json")
	if _, err := os.Stat(credsFile); err != nil {
		t.Errorf("expected oauth_creds.json at %s: %v", credsFile, err)
	}
	gaFile := filepath.Join(expectedAgyDir, ".gemini", "google_accounts.json")
	if _, err := os.Stat(gaFile); err != nil {
		t.Errorf("expected google_accounts.json at %s: %v", gaFile, err)
	}

	// Verify RTK projection for Claude
	home, _ := os.UserHomeDir()
	if _, err := os.Stat(filepath.Join(home, ".claude", "RTK.md")); err == nil {
		projectedRTK := filepath.Join(expectedClaudeDir, "RTK.md")
		if _, err := os.Lstat(projectedRTK); err != nil {
			t.Errorf("expected RTK.md projected in claude sandbox: %v", err)
		}
	}

	// Verify Claude credentials projection
	if _, err := os.Stat(filepath.Join(home, ".claude.json")); err == nil {
		claudeJSON := filepath.Join(expectedClaudeDir, ".claude.json")
		if _, err := os.Lstat(claudeJSON); err != nil {
			t.Errorf("expected .claude.json projected into claude sandbox: %v", err)
		}
	}

	// Test Codex credentials projection
	_, err = v.PrepareSandboxWithSkills("codex", "default", nil)
	if err != nil {
		t.Fatalf("prepare codex sandbox failed: %v", err)
	}
	expectedCodexDir := filepath.Join(harnessesDir, "codex", "default")
	if _, err := os.Stat(filepath.Join(home, ".codex", "auth.json")); err == nil {
		authLink := filepath.Join(expectedCodexDir, "auth.json")
		if _, err := os.Lstat(authLink); err != nil {
			t.Errorf("expected auth.json projected into codex sandbox: %v", err)
		}
	}

	// Test Agy fallback without token (must not have "active": "default")
	if err := v.PrepareAgyAccount("default", "", "default"); err != nil {
		t.Fatalf("prepare default agy account failed: %v", err)
	}
	defaultAgyDir := filepath.Join(harnessesDir, "agy", "default")
	defaultGA := filepath.Join(defaultAgyDir, ".gemini", "google_accounts.json")
	if data, err := os.ReadFile(defaultGA); err == nil {
		if string(data) == `{"active":"default","old":[]}` || string(data) == "{\n  \"active\": \"default\",\n  \"old\": []\n}" {
			t.Errorf("google_accounts.json has invalid active: default in sandbox")
		}
	}
}

func TestVirtualizer_ToolPermissionsReplication(t *testing.T) {
	tempDir := t.TempDir()
	harnessesDir := filepath.Join(tempDir, "harnesses")
	v := NewVirtualizer(harnessesDir, "")

	testProject := filepath.Join(tempDir, "my-super-project")
	if err := os.MkdirAll(testProject, 0755); err != nil {
		t.Fatal(err)
	}

	// 1. Test TrustProjectFolder
	if err := v.TrustProjectFolder(testProject); err != nil {
		t.Fatalf("TrustProjectFolder failed: %v", err)
	}

	// 2. Prepare an agy sandbox and check permissions replication
	_, err := v.PrepareSandboxWithSkills("agy", "acc1", nil)
	if err != nil {
		t.Fatalf("PrepareSandboxWithSkills failed: %v", err)
	}

	if err := v.ReplicateToolPermissions("agy", "acc1", testProject); err != nil {
		t.Fatalf("ReplicateToolPermissions failed: %v", err)
	}

	sandboxTF := filepath.Join(harnessesDir, "agy", "acc1", ".gemini", "trustedFolders.json")
	if data, err := os.ReadFile(sandboxTF); err != nil {
		t.Fatalf("failed to read sandbox trustedFolders.json: %v", err)
	} else if len(data) == 0 {
		t.Errorf("expected non-empty sandbox trustedFolders.json")
	}

	// 3. Test SyncAllAccountPermissions
	if err := v.SyncAllAccountPermissions([]string{testProject}); err != nil {
		t.Fatalf("SyncAllAccountPermissions failed: %v", err)
	}
}
