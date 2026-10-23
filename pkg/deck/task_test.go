package deck

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/falluba/ai-desk/pkg/harness"
)

func TestCreateTask(t *testing.T) {
	sdb, _ := setupTestDB(t)
	defer sdb.Close()

	tempHarnessDir := t.TempDir()
	tempSkillsDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tempSkillsDir, "shadcn"), 0755)
	_ = os.MkdirAll(filepath.Join(tempSkillsDir, "auth"), 0755)

	virt := harness.NewVirtualizer(tempHarnessDir, tempSkillsDir)

	opts := CreateTaskOptions{
		Title:         "Frontend Auth Revamp",
		ProjectPath:   t.TempDir(),
		GroupPath:     "Company/WebApps/Frontend",
		Tool:          "agy",
		Account:       "tester@example.com",
		SkillProfile:  "core",
		InitialPrompt: "Review login page",
	}

	inst, err := sdb.CreateTask(virt, opts)
	if err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	if inst == nil {
		t.Fatal("expected non-nil instance record")
	}

	if inst.Title != "Frontend Auth Revamp" {
		t.Errorf("expected title %q, got %q", opts.Title, inst.Title)
	}
	if inst.GroupPath != "Company/WebApps/Frontend" {
		t.Errorf("expected group_path %q, got %q", opts.GroupPath, inst.GroupPath)
	}
	if inst.Tool != "agy" {
		t.Errorf("expected tool %q, got %q", opts.Tool, inst.Tool)
	}
	if inst.Account != "tester@example.com" {
		t.Errorf("expected account %q, got %q", opts.Account, inst.Account)
	}

	// Verify group hierarchy created
	groups, err := sdb.LoadGroups()
	if err != nil {
		t.Fatalf("load groups failed: %v", err)
	}
	// "Company", "Company/WebApps", "Company/WebApps/Frontend"
	found := false
	for _, g := range groups {
		if g.Path == "Company/WebApps/Frontend" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected group 'Company/WebApps/Frontend' to exist in groups table")
	}

	// Verify persisted instance in database
	dbInst, err := sdb.FindInstance(inst.ID)
	if err != nil {
		t.Fatalf("FindInstance failed: %v", err)
	}
	if dbInst.Title != inst.Title {
		t.Errorf("expected DB title %q, got %q", inst.Title, dbInst.Title)
	}

	// Verify tool_data contains profile
	var td map[string]interface{}
	if err := json.Unmarshal([]byte(dbInst.ToolData), &td); err != nil {
		t.Fatalf("invalid tool_data JSON: %v", err)
	}
	if td["skill_profile"] != "core" {
		t.Errorf("expected skill_profile 'core', got %v", td["skill_profile"])
	}
	if td["initial_prompt"] != "Review login page" {
		t.Errorf("expected initial_prompt 'Review login page', got %v", td["initial_prompt"])
	}

	// Verify sandbox directory was created
	sandbox := virt.SandboxPath("agy", "tester@example.com")
	if _, err := os.Stat(sandbox); err != nil {
		t.Errorf("expected sandbox directory to exist at %s", sandbox)
	}
}

func TestCreateTask_VoxStudioAndMultiSkills(t *testing.T) {
	sdb, _ := setupTestDB(t)
	defer sdb.Close()

	tempHarnessDir := t.TempDir()
	tempSkillsDir := t.TempDir()
	_ = os.MkdirAll(filepath.Join(tempSkillsDir, "shadcn"), 0755)
	_ = os.MkdirAll(filepath.Join(tempSkillsDir, "hyperframes"), 0755)

	virt := harness.NewVirtualizer(tempHarnessDir, tempSkillsDir)

	opts := CreateTaskOptions{
		Title:        "Product Explainer",
		Tool:         "vox-studio",
		SkillProfile: "core,video",
	}

	inst, err := sdb.CreateTask(virt, opts)
	if err != nil {
		t.Fatalf("CreateTask vox-studio failed: %v", err)
	}

	if inst.Tool != "vox-studio" {
		t.Errorf("expected tool vox-studio, got %q", inst.Tool)
	}
	if !strings.Contains(inst.Command, "launch.ts") {
		t.Errorf("expected command to mention launch.ts, got %q", inst.Command)
	}
	if inst.GroupPath != "Creative/VideoStudio" {
		t.Errorf("expected group Creative/VideoStudio, got %q", inst.GroupPath)
	}
}
