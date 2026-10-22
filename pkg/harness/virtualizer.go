package harness

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Virtualizer manages isolated sandbox directories for AI CLI harnesses,
// preventing concurrent execution token clashes and projecting shared skills.
type Virtualizer struct {
	baseDir    string // e.g. ~/.ai-desk/harnesses
	skillsPath string // e.g. /home/falluba/.agents/skills
	agentsMD   string // e.g. /home/falluba/.agents/AGENTS.md
}

// NewVirtualizer creates a new Virtualizer instance.
func NewVirtualizer(baseDir, skillsPath string) *Virtualizer {
	home, _ := os.UserHomeDir()
	if baseDir == "" {
		baseDir = filepath.Join(home, ".ai-desk", "harnesses")
	}
	if skillsPath == "" {
		skillsPath = filepath.Join(home, ".agents", "skills")
	}
	agentsMD := filepath.Join(home, ".agents", "AGENTS.md")

	return &Virtualizer{
		baseDir:    baseDir,
		skillsPath: skillsPath,
		agentsMD:   agentsMD,
	}
}

// SandboxPath returns the dedicated configuration directory for a tool and account.
func (v *Virtualizer) SandboxPath(tool, accountID string) string {
	if accountID == "" {
		accountID = "default"
	}
	return filepath.Join(v.baseDir, tool, accountID)
}

// PrepareSandbox prepares the sandbox with all available skills (legacy/full mode).
func (v *Virtualizer) PrepareSandbox(tool, accountID string) (map[string]string, error) {
	allSkills, err := ListAllSkillNames(v.skillsPath)
	if err != nil {
		allSkills = nil
	}
	return v.PrepareSandboxWithSkills(tool, accountID, allSkills)
}

// PrepareSandboxWithSkills ensures the sandbox directory exists, projects only
// the specified subset of skills, and returns the required environment overrides.
func (v *Virtualizer) PrepareSandboxWithSkills(tool, accountID string, skillNames []string) (map[string]string, error) {
	sandbox := v.SandboxPath(tool, accountID)
	if err := os.MkdirAll(sandbox, 0700); err != nil {
		return nil, fmt.Errorf("create sandbox %s: %w", sandbox, err)
	}

	targetSkillsDir := filepath.Join(sandbox, "skills")
	if err := os.MkdirAll(targetSkillsDir, 0700); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}

	// Symlink only the chosen skills
	if v.skillsPath != "" {
		for _, name := range skillNames {
			src := filepath.Join(v.skillsPath, name)
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				v.ensureSymlink(src, filepath.Join(targetSkillsDir, name))
			}
		}

		// Tool-specific discovery paths for Antigravity:
		if tool == "agy" || tool == "gemini" {
			_ = os.MkdirAll(filepath.Join(sandbox, "antigravity-cli"), 0700)
			_ = os.MkdirAll(filepath.Join(sandbox, "config"), 0700)
			v.ensureSymlink(targetSkillsDir, filepath.Join(sandbox, "antigravity-cli", "skills"))
			v.ensureSymlink(targetSkillsDir, filepath.Join(sandbox, "config", "skills"))
		}
	}

	// Project shared instructions (~/.agents/AGENTS.md)
	if v.agentsMD != "" {
		if _, err := os.Stat(v.agentsMD); err == nil {
			v.ensureSymlink(v.agentsMD, filepath.Join(sandbox, "AGENTS.md"))
			v.ensureSymlink(v.agentsMD, filepath.Join(sandbox, "CLAUDE.md"))
		}
	}

	env := make(map[string]string)
	switch tool {
	case "claude":
		env["CLAUDE_CONFIG_DIR"] = sandbox
	case "agy", "gemini":
		env["GEMINI_CLI_HOME"] = sandbox
	case "codex":
		env["CODEX_HOME"] = sandbox
	case "opencode":
		env["OPENCODE_CONFIG_DIR"] = sandbox
	}
	return env, nil
}

// AttachSkill dynamically adds an extra skill to an active session sandbox at runtime.
func (v *Virtualizer) AttachSkill(tool, accountID, skillName string) error {
	src := filepath.Join(v.skillsPath, skillName)
	if info, err := os.Stat(src); err != nil || !info.IsDir() {
		return fmt.Errorf("skill %q not found in %s", skillName, v.skillsPath)
	}

	sandbox := v.SandboxPath(tool, accountID)
	targetDir := filepath.Join(sandbox, "skills")
	if err := os.MkdirAll(targetDir, 0700); err != nil {
		return err
	}

	dst := filepath.Join(targetDir, skillName)
	if _, err := os.Lstat(dst); err == nil {
		return nil // already attached
	}
	return os.Symlink(src, dst)
}

// DetachSkill removes a skill from a session sandbox at runtime.
func (v *Virtualizer) DetachSkill(tool, accountID, skillName string) error {
	sandbox := v.SandboxPath(tool, accountID)
	dst := filepath.Join(sandbox, "skills", skillName)
	if _, err := os.Lstat(dst); err == nil {
		return os.Remove(dst)
	}
	return nil
}

// ListAttachedSkills returns the names of all skills currently attached to a session sandbox.
func (v *Virtualizer) ListAttachedSkills(tool, accountID string) ([]string, error) {
	sandbox := v.SandboxPath(tool, accountID)
	targetDir := filepath.Join(sandbox, "skills")
	entries, err := os.ReadDir(targetDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

func (v *Virtualizer) ensureSymlink(target, linkPath string) {
	if _, lerr := os.Lstat(linkPath); os.IsNotExist(lerr) {
		_ = os.Symlink(target, linkPath)
	}
}

// WriteAgyToken writes an isolated Google Antigravity credential inside the sandbox.
func (v *Virtualizer) WriteAgyToken(accountID, tokenJSON, email string) error {
	sandbox := v.SandboxPath("agy", accountID)
	if err := os.MkdirAll(sandbox, 0700); err != nil {
		return err
	}
	// Write auth token file
	if tokenJSON != "" {
		tokenFile := filepath.Join(sandbox, "oauth_tokens.json")
		if err := os.WriteFile(tokenFile, []byte(tokenJSON), 0600); err != nil {
			return fmt.Errorf("write token: %w", err)
		}
	}
	// Write active account hint
	if email != "" {
		hintFile := filepath.Join(sandbox, "active_account.json")
		hintData, _ := json.Marshal(map[string]string{"email": email})
		_ = os.WriteFile(hintFile, hintData, 0600)
	}
	return nil
}
