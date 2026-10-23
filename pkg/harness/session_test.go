package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestNamedCredentialsNeverUseHostLogin(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AI_DESK_ACCOUNTS_DIR", filepath.Join(home, "accounts"))
	_ = os.MkdirAll(filepath.Join(home, ".codex"), 0700)
	_ = os.WriteFile(filepath.Join(home, ".codex", "auth.json"), []byte(`{"host":true}`), 0600)
	v := NewVirtualizer(t.TempDir(), t.TempDir()).ForSession("session")
	if err := v.PrepareCredentials("codex", "missing"); err == nil {
		t.Fatal("missing identity accepted")
	}
	if _, err := v.PrepareSandboxWithSkills("codex", "missing", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(v.SandboxPath("codex", "missing"), "auth.json")); !os.IsNotExist(err) {
		t.Fatal("projected host login for named account")
	}
}

func TestNoneAndInvalidProfiles(t *testing.T) {
	cfg := defaultProfilesConfig()
	if skills, err := cfg.ResolveSkills("none", t.TempDir()); err != nil || len(skills) != 0 {
		t.Fatalf("%v %v", skills, err)
	}
	if _, err := cfg.ResolveSkills("core,typo", t.TempDir()); err == nil {
		t.Fatal("partial resolution silently accepted")
	}
	v := NewVirtualizer(t.TempDir(), t.TempDir())
	if err := v.AttachSkill("codex", "default", "../escape"); err == nil {
		t.Fatal("path traversal accepted")
	}
}
