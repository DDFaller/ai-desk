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

	// 2b. Test multi-profile union resolution ("lean,extra")
	cfg.Profiles["extra"] = ProfileDefinition{
		Description: "Extra profile",
		Skills:      []string{"skill-b"},
	}
	combined, err := cfg.ResolveSkills("lean, extra", canonicalSkillsDir)
	if err != nil {
		t.Fatalf("resolve multi-profile: %v", err)
	}
	if len(combined) != 2 {
		t.Fatalf("expected 2 skills for lean+extra, got %v", combined)
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

	// 6. Test pruning when re-preparing sandbox with different skills (e.g. only skill-c)
	_, err = v.PrepareSandboxWithSkills("claude", "test-acc", []string{"skill-c"})
	if err != nil {
		t.Fatalf("prepare sandbox with skill-c: %v", err)
	}
	attachedPruned, _ := v.ListAttachedSkills("claude", "test-acc")
	if len(attachedPruned) != 1 || attachedPruned[0] != "skill-c" {
		t.Fatalf("expected only [skill-c] after re-scoping, got %v", attachedPruned)
	}

	// 7. Test legacy symlink cleanup
	legacyAcc := "legacy-acc"
	legacyDir := v.SandboxPath("claude", legacyAcc)
	_ = os.MkdirAll(legacyDir, 0755)
	legacySkillsSymlink := filepath.Join(legacyDir, "skills")
	_ = os.Symlink(canonicalSkillsDir, legacySkillsSymlink)

	_, err = v.PrepareSandboxWithSkills("claude", legacyAcc, []string{"skill-b"})
	if err != nil {
		t.Fatalf("prepare sandbox on legacy symlink: %v", err)
	}
	legacyAttached, _ := v.ListAttachedSkills("claude", legacyAcc)
	if len(legacyAttached) != 1 || legacyAttached[0] != "skill-b" {
		t.Fatalf("expected legacy symlink replaced with isolated [skill-b], got %v", legacyAttached)
	}

	// 8. Test that hidden directories are excluded from ListAllSkillNames
	_ = os.MkdirAll(filepath.Join(canonicalSkillsDir, ".system"), 0755)
	names, _ := ListAllSkillNames(canonicalSkillsDir)
	for _, n := range names {
		if n == ".system" {
			t.Fatalf("expected .system to be excluded from ListAllSkillNames, got %v", names)
		}
	}
}
