package deck

import (
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEmptySkillsStayEmptyAndSessionsAreIsolated(t *testing.T) {
	db, _ := setupTestDB(t)
	defer db.Close()
	v := harness.NewVirtualizer(t.TempDir(), t.TempDir())
	_ = os.MkdirAll(filepath.Join(v.SkillsPath(), "extra"), 0700)
	first, err := db.CreateTask(v, CreateTaskOptions{Title: "empty", Tool: "codex", ProjectPath: t.TempDir(), SkillProfile: "none"})
	if err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateTask(v, CreateTaskOptions{Title: "other", Tool: "codex", ProjectPath: t.TempDir(), SelectedSkills: []string{"extra"}})
	if err != nil {
		t.Fatal(err)
	}
	if v.ForSession(first.ID).SandboxPath("codex", "default") == v.ForSession(second.ID).SandboxPath("codex", "default") {
		t.Fatal("sessions share sandbox")
	}
	skills, err := SessionSkills(v, first)
	if err != nil || len(skills) != 0 {
		t.Fatalf("empty became %v: %v", skills, err)
	}
	if err = db.SetSessionSkills(v, first.ID, "custom", []string{"extra"}); err != nil {
		t.Fatal(err)
	}
	first, _ = db.FindInstance(first.ID)
	cfg, _ := ReadSessionConfig(first)
	if !cfg.SkillsPending {
		t.Fatal("must require restart")
	}
	active, _ := v.ForSession(first.ID).ListAttachedSkills("codex", "default")
	if len(active) != 0 {
		t.Fatal("staging mutated live skills")
	}
	second, _ = db.FindInstance(second.ID)
	other, _ := SessionSkills(v, second)
	if len(other) != 1 {
		t.Fatal("other session changed")
	}
	if err = db.SetSessionSkills(v, first.ID, "none", nil); err != nil {
		t.Fatal(err)
	}
	updated, err := SwitchAccount(db, v, first.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = ReadSessionConfig(updated)
	if len(cfg.SelectedSkills) != 0 || cfg.SkillsPending {
		t.Fatalf("restart lost empty state: %+v", cfg)
	}
}

func TestSessionProfileRejectsOutsideAccounts(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	cfg := rotation.DefaultAccountProfilesConfig()
	cfg.Profiles["locked"] = rotation.AccountProfile{Name: "locked", Accounts: []string{"allowed"}, Policy: rotation.PolicySticky, AutoFailover: true}
	if err := cfg.Save(""); err != nil {
		t.Fatal(err)
	}
	t.Setenv("AI_DESK_ACCOUNTS_DIR", filepath.Join(home, "accounts"))
	cred := filepath.Join(home, "auth.json")
	_ = os.WriteFile(cred, []byte(`{"OPENAI_API_KEY":"fixture"}`), 0600)
	for _, id := range []string{"allowed", "outside"} {
		if err := harness.ImportAccount("codex", id, cred); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := RouteAccount("codex", "locked", "outside"); err == nil {
		t.Fatal("escaped profile")
	}
	if got, err := RouteAccount("codex", "locked", ""); err != nil || got != "allowed" {
		t.Fatalf("%q: %v", got, err)
	}
	if _, err := RouteAccount("codex", "missing", ""); err == nil {
		t.Fatal("unknown profile accepted")
	}
}

func TestFailedReplacementKeepsOldSession(t *testing.T) {
	db, _ := setupTestDBWithInstance(t)
	defer db.Close()
	_, err := db.db.Exec("UPDATE instances SET tmux_session = 'old-live' WHERE id = 'test-inst-1'")
	if err != nil {
		t.Fatal(err)
	}
	bin := t.TempDir()
	log := filepath.Join(bin, "calls")
	script := "#!/bin/sh\nif [ \"$1\" = -L ]; then shift 2; fi\nprintf '%s\\n' \"$*\" >> '" + log + "'\ncase \"$1\" in new-session) exit 1;; esac\nexit 0\n"
	_ = os.WriteFile(filepath.Join(bin, "tmux"), []byte(script), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	_, err = SwitchHarness(db, harness.NewVirtualizer(t.TempDir(), t.TempDir()), "test-inst-1", SwitchOptions{TargetTool: "claude"})
	if err == nil {
		t.Fatal("launch failure reported as success")
	}
	calls, _ := os.ReadFile(log)
	if strings.Contains(string(calls), "kill-session") {
		t.Fatal("old session killed before replacement was available")
	}
	inst, _ := db.FindInstance("test-inst-1")
	if inst.TmuxSession != "old-live" || inst.Tool != "codex" {
		t.Fatal("failed replacement modified persisted identity")
	}
}
