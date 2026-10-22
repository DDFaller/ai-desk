package harness

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSkillsCatalog_ProfilesAndAttach(t *testing.T) {
	tempDir := t.TempDir()
	canonicalSkillsDir := filepath.Join(tempDir, "canonical_skills")
	_ = os.MkdirAll(filepath.Join(canonicalSkillsDir, "skill-a"), 0755)
	_ = os.MkdirAll(filepath.Join(canonicalSkillsDir, "skill-b"), 0755)
	_ = os.MkdirAll(filepath.Join(canonicalSkillsDir, "skill-c"), 0755)

	profilesYAML := `
profiles:
  lean:
    description: "Lean test profile"
    skills:
      - skill-a
  full:
    description: "All skills"
    skills:
      - "*"
`
	profilesPath := filepath.Join(tempDir, "profiles.yaml")
	_ = os.WriteFile(profilesPath, []byte(profilesYAML), 0644)

	cfg, err := LoadSkillProfiles(profilesPath)
	if err != nil {
		t.Fatalf("load profiles: %v", err)
	}

	// 1. Resolve lean profile
	skills, err := cfg.ResolveSkills("lean", canonicalSkillsDir)
	if err != nil {
		t.Fatalf("resolve lean: %v", err)
	}
	if len(skills) != 1 || skills[0] != "skill-a" {
		t.Fatalf("expected [skill-a], got %v", skills)
	}

	// 2. Resolve wildcard profile
	allSkills, err := cfg.ResolveSkills("full", canonicalSkillsDir)
	if err != nil {
		t.Fatalf("resolve full: %v", err)
	}
	if len(allSkills) != 3 {
		t.Fatalf("expected 3 skills for full, got %d", len(allSkills))
	}

	// 3. Test Sandbox creation with lean profile
	harnessesDir := filepath.Join(tempDir, "harnesses")
	v := NewVirtualizer(harnessesDir, canonicalSkillsDir)
	_, err = v.PrepareSandboxWithSkills("claude", "test-acc", skills)
	if err != nil {
		t.Fatalf("prepare sandbox: %v", err)
	}

	attached, err := v.ListAttachedSkills("claude", "test-acc")
	if err != nil || len(attached) != 1 || attached[0] != "skill-a" {
		t.Fatalf("expected attached [skill-a], got %v", attached)
	}

	// 4. Test conscious runtime attachment of skill-b
	if err := v.AttachSkill("claude", "test-acc", "skill-b"); err != nil {
		t.Fatalf("attach skill-b: %v", err)
	}
	attachedAfter, _ := v.ListAttachedSkills("claude", "test-acc")
	if len(attachedAfter) != 2 {
		t.Fatalf("expected 2 attached skills after attach, got %d", len(attachedAfter))
	}

	// 5. Test detach skill-b
	if err := v.DetachSkill("claude", "test-acc", "skill-b"); err != nil {
		t.Fatalf("detach skill-b: %v", err)
	}
	attachedFinal, _ := v.ListAttachedSkills("claude", "test-acc")
	if len(attachedFinal) != 1 {
		t.Fatalf("expected 1 attached skill after detach, got %d", len(attachedFinal))
	}
}
