package deck

import (
	"github.com/falluba/ai-desk/pkg/harness"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func agentTestRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		out, err := exec.Command("git", append([]string{"-C", repo}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("git: %v %s", err, out)
		}
	}
	run("init", "-q")
	_ = os.WriteFile(filepath.Join(repo, "AGENTS.md"), []byte("Project instructions must survive.\n"), 0600)
	run("add", "AGENTS.md")
	run("-c", "user.name=Fixture", "-c", "user.email=fixture@example.test", "-c", "commit.gpgsign=false", "commit", "-qm", "fixture")
	return repo
}

func TestAgentWorktreeMemoryAndSessionInheritance(t *testing.T) {
	t.Setenv("AI_DESK_AGENTS_DIR", t.TempDir())
	db, _ := setupTestDB(t)
	defer db.Close()
	repo := agentTestRepo(t)
	a, err := db.CreateAgent(CreateAgentOptions{Name: "Builder", Role: "Maintain tests", Repository: repo, Runtime: "codex", SkillProfile: "none"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Worktree == repo {
		t.Fatal("agent reuses source checkout")
	}
	memory := filepath.Join(a.Home, "memory", "MEMORY.md")
	_ = os.WriteFile(memory, []byte("Remember the integration socket."), 0600)
	v := harness.NewVirtualizer(t.TempDir(), t.TempDir())
	inst, err := db.CreateTask(v, CreateTaskOptions{AgentID: a.ID})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.UpdateAgentDefaults(a.ID, "claude", "", "minimal"); err != nil {
		t.Fatal(err)
	}
	cfg, _ := ReadSessionConfig(inst)
	if cfg.AgentID != a.ID || cfg.SkillProfile != "none" || inst.ProjectPath != a.Worktree {
		t.Fatalf("defaults lost: %+v %+v", inst, cfg)
	}
	bridge := filepath.Join(v.ForSession(inst.ID).SandboxPath("codex", "default"), "AGENTS.md")
	data, err := os.ReadFile(bridge)
	if err != nil || !strings.Contains(string(data), "integration socket") {
		t.Fatalf("missing memory bridge: %s %v", data, err)
	}
	updated, err := SwitchHarness(db, v, inst.ID, SwitchOptions{TargetTool: "claude", PreserveSkills: true})
	if err != nil {
		t.Fatal(err)
	}
	cfg, _ = ReadSessionConfig(updated.Instance)
	if cfg.AgentID != a.ID {
		t.Fatal("identity lost on switch")
	}
	data, err = os.ReadFile(filepath.Join(SessionVirtualizer(v, updated.Instance).SandboxPath("claude", "default"), "CLAUDE.md"))
	if err != nil || !strings.Contains(string(data), "integration socket") {
		t.Fatal("memory lost across runtime", err)
	}
	original, _ := os.ReadFile(filepath.Join(a.Worktree, "AGENTS.md"))
	if string(original) != "Project instructions must survive.\n" {
		t.Fatal("project instructions overwritten")
	}
	if err = db.ArchiveAgent(a.ID, true); err != nil {
		t.Fatal(err)
	}
	if _, err = db.CreateTask(v, CreateTaskOptions{AgentID: a.ID}); err == nil {
		t.Fatal("archived agent started a session")
	}
	if _, err = os.Stat(memory); err != nil {
		t.Fatal("archive deleted memory")
	}
}

func TestAdoptionAndDirtyWorktreeProtection(t *testing.T) {
	t.Setenv("AI_DESK_AGENTS_DIR", t.TempDir())
	db, _ := setupTestDBWithInstance(t)
	defer db.Close()
	a, err := db.CreateAgent(CreateAgentOptions{Name: "Keeper", Repository: agentTestRepo(t)})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := db.FindInstance("test-inst-1")
	if err = db.AdoptSession(a.ID, before.ID); err != nil {
		t.Fatal(err)
	}
	after, _ := db.FindInstance(before.ID)
	if after.ProjectPath != before.ProjectPath {
		t.Fatal("adoption moved existing session")
	}
	_ = db.ArchiveAgent(a.ID, true)
	_ = os.WriteFile(filepath.Join(a.Worktree, "untracked.txt"), []byte("keep"), 0600)
	if err = db.RemoveAgentWorktree(a.ID); err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("dirty worktree not protected: %v", err)
	}
}
