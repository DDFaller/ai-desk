package deck

import (
	"fmt"
	"github.com/falluba/ai-desk/pkg/harness"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestRealTmuxSessionIsolation(t *testing.T) {
	if os.Getenv("AI_DESK_INTEGRATION") != "1" {
		t.Skip("set AI_DESK_INTEGRATION=1 to test real tmux with fixture CLIs")
	}
	realTmux := "/usr/bin/tmux"
	if _, err := os.Stat(realTmux); err != nil {
		t.Fatal(err)
	}
	socket := fmt.Sprintf("ai-desk-test-%d", time.Now().UnixNano())
	t.Cleanup(func() { _ = exec.Command(realTmux, "-L", socket, "kill-server").Run() })
	bin := t.TempDir()
	wrapper := "#!/bin/sh\nexec " + realTmux + " -L " + socket + " \"$@\"\n"
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(wrapper), 0700); err != nil {
		t.Fatal(err)
	}
	cli := "#!/bin/sh\nprintf 'fixture-ready\\n'\nwhile :; do sleep 1; done\n"
	_ = os.WriteFile(filepath.Join(bin, "codex"), []byte(cli), 0700)
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	db, _ := setupTestDB(t)
	defer db.Close()
	v := harness.NewVirtualizer(t.TempDir(), t.TempDir())
	first, err := db.CreateTask(v, CreateTaskOptions{Title: "isolated", Tool: "codex", ProjectPath: t.TempDir(), SkillProfile: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if err := exec.Command(realTmux, "-L", socket, "has-session", "-t", "="+first.TmuxSession).Run(); err != nil {
		t.Fatal(err)
	}
	second, err := db.CreateTask(v, CreateTaskOptions{Title: "neighbor", Tool: "codex", ProjectPath: t.TempDir(), SkillProfile: "none"})
	if err != nil {
		t.Fatal(err)
	}
	updated, err := SwitchAccount(db, v, first.ID, "default", true)
	if err != nil {
		t.Fatal(err)
	}
	if updated.TmuxSession == first.TmuxSession {
		t.Fatal("replacement did not get distinct process identity")
	}
	if err := exec.Command(realTmux, "-L", socket, "has-session", "-t", "="+second.TmuxSession).Run(); err != nil {
		t.Fatal("neighbor was killed", err)
	}
	cfg, _ := ReadSessionConfig(updated)
	if cfg.SkillProfile != "none" || len(cfg.SelectedSkills) != 0 {
		t.Fatalf("empty skills changed: %+v", cfg)
	}
}
