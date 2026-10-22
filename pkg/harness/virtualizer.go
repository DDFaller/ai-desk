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
}

// NewVirtualizer creates a new Virtualizer instance.
func NewVirtualizer(baseDir, skillsPath string) *Virtualizer {
	if baseDir == "" {
		home, _ := os.UserHomeDir()
		baseDir = filepath.Join(home, ".ai-desk", "harnesses")
	}
	if skillsPath == "" {
		home, _ := os.UserHomeDir()
		skillsPath = filepath.Join(home, ".agents", "skills")
	}
	return &Virtualizer{
		baseDir:    baseDir,
		skillsPath: skillsPath,
	}
}

// SandboxPath returns the dedicated configuration directory for a tool and account.
func (v *Virtualizer) SandboxPath(tool, accountID string) string {
	if accountID == "" {
		accountID = "default"
	}
	return filepath.Join(v.baseDir, tool, accountID)
}

// PrepareSandbox ensures the sandbox directory exists, links shared skills,
// and returns the environment variables required for the harness.
func (v *Virtualizer) PrepareSandbox(tool, accountID string) (map[string]string, error) {
	sandbox := v.SandboxPath(tool, accountID)
	if err := os.MkdirAll(sandbox, 0700); err != nil {
		return nil, fmt.Errorf("create sandbox %s: %w", sandbox, err)
	}

	// Project canonical ~/.agents/skills via symbolic link
	if v.skillsPath != "" {
		if _, err := os.Stat(v.skillsPath); err == nil {
			targetSkills := filepath.Join(sandbox, "skills")
			if _, lerr := os.Lstat(targetSkills); os.IsNotExist(lerr) {
				_ = os.Symlink(v.skillsPath, targetSkills)
			}
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
