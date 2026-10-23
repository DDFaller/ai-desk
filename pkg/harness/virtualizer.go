package harness

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"github.com/falluba/ai-desk/pkg/configpaths"
	"os"
	"path/filepath"
	"strings"
)

// sanitizeAccountID converts an account email or ID into a safe directory name.
func sanitizeAccountID(accountID string) string {
	if accountID == "" {
		return "default"
	}
	r := strings.NewReplacer("@", "_", ":", "_", "/", "_", "\\", "_")
	return r.Replace(accountID)
}

// Virtualizer manages isolated sandbox directories for AI CLI harnesses,
// preventing concurrent execution token clashes and projecting shared skills.
type Virtualizer struct {
	baseDir    string // e.g. ~/.ai-desk/harnesses
	skillsPath string // e.g. /home/falluba/.agents/skills
	agentsMD   string // e.g. /home/falluba/.agents/AGENTS.md
}

// NewVirtualizer creates a new Virtualizer instance.
func NewVirtualizer(baseDir, skillsPath string) *Virtualizer {
	home, _ := configpaths.HomeDir()
	if baseDir == "" {
		baseDir = os.Getenv("AI_DESK_HARNESSES_DIR")
	}
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

// ForSession isolates mutable configuration and skills from other sessions.
func (v *Virtualizer) ForSession(id string) *Virtualizer {
	if id == "" {
		return v
	}
	clone := *v
	sum := sha256.Sum256([]byte(id))
	clone.baseDir = filepath.Join(v.baseDir, "sessions", fmt.Sprintf("%x", sum[:16]))
	return &clone
}

// BaseDir returns the root directory where isolated tool sandboxes reside.
func (v *Virtualizer) BaseDir() string {
	return v.baseDir
}

// SkillsPath returns the source directory for shared agent skills.
func (v *Virtualizer) SkillsPath() string {
	return v.skillsPath
}

// SandboxPath returns the dedicated configuration directory for a tool and account.
func (v *Virtualizer) SandboxPath(tool, accountID string) string {
	safeAccount := sanitizeAccountID(accountID)
	return filepath.Join(v.baseDir, tool, safeAccount)
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
	for _, name := range skillNames {
		if err := ValidateSkillName(name); err != nil {
			return nil, err
		}
	}
	sandbox := v.SandboxPath(tool, accountID)
	if err := os.MkdirAll(sandbox, 0700); err != nil {
		return nil, fmt.Errorf("create sandbox %s: %w", sandbox, err)
	}

	targetSkillsDir := filepath.Join(sandbox, "skills")
	// If targetSkillsDir is a symlink (e.g. legacy symlink to ~/.agents/skills), remove it!
	if fi, err := os.Lstat(targetSkillsDir); err == nil && (fi.Mode()&os.ModeSymlink != 0) {
		_ = os.Remove(targetSkillsDir)
	}
	if err := os.MkdirAll(targetSkillsDir, 0700); err != nil {
		return nil, fmt.Errorf("create skills dir: %w", err)
	}

	// Prune any existing symlink in targetSkillsDir that is not in skillNames
	wanted := make(map[string]bool)
	for _, s := range skillNames {
		wanted[s] = true
	}
	if entries, err := os.ReadDir(targetSkillsDir); err == nil {
		for _, e := range entries {
			if !wanted[e.Name()] {
				_ = os.Remove(filepath.Join(targetSkillsDir, e.Name()))
			}
		}
	}

	// Symlink only the chosen skills
	if v.skillsPath != "" {
		for _, name := range skillNames {
			src := filepath.Join(v.skillsPath, name)
			if info, err := os.Stat(src); err == nil && info.IsDir() {
				v.ensureSymlink(src, filepath.Join(targetSkillsDir, name))
			}
		}

		// Also create sandbox/.agents/skills -> targetSkillsDir so any tool that checks ~/.agents/skills
		// will only see the scoped skills when HOME=sandbox!
		agentsDir := filepath.Join(sandbox, ".agents")
		_ = os.MkdirAll(agentsDir, 0700)
		v.ensureSymlink(targetSkillsDir, filepath.Join(agentsDir, "skills"))

		// Tool-specific discovery paths for Claude:
		if tool == "claude" {
			claudeDir := filepath.Join(sandbox, ".claude")
			_ = os.MkdirAll(claudeDir, 0700)
			v.ensureSymlink(targetSkillsDir, filepath.Join(claudeDir, "skills"))
		}

		// Tool-specific discovery paths for Codex:
		if tool == "codex" {
			codexDir := filepath.Join(sandbox, ".codex")
			_ = os.MkdirAll(codexDir, 0700)
			v.ensureSymlink(targetSkillsDir, filepath.Join(codexDir, "skills"))
		}

		// Tool-specific discovery paths for Antigravity:
		if tool == "agy" || tool == "gemini" {
			geminiDir := filepath.Join(sandbox, ".gemini")
			cliDir := filepath.Join(geminiDir, "antigravity-cli")
			cfgDir := filepath.Join(geminiDir, "config")
			_ = os.MkdirAll(cliDir, 0700)
			_ = os.MkdirAll(cfgDir, 0700)
			v.ensureSymlink(targetSkillsDir, filepath.Join(cliDir, "skills"))
			v.ensureSymlink(targetSkillsDir, filepath.Join(cfgDir, "skills"))

			// Also legacy sandbox/antigravity-cli and sandbox/config
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

	// Project RTK (Rust Token Killer) hooks and configurations for token optimization
	v.projectRTK(tool, sandbox)

	// Symlink host dotfiles into sandbox so shell, git, and path tools function smoothly
	v.projectHostDotfiles(sandbox)

	// Project host credentials and configuration into sandbox
	v.projectCredentials(tool, accountID, sandbox)
	_ = v.ReplicateToolPermissions(tool, accountID, "")

	env := make(map[string]string)
	switch tool {
	case "claude":
		env["HOME"] = sandbox
		env["CLAUDE_CONFIG_DIR"] = sandbox
		env["AI_DESK_ACCOUNT"] = accountID
	case "agy", "gemini":
		env["HOME"] = sandbox
		env["GEMINI_CLI_HOME"] = sandbox
		env["AI_DESK_ACCOUNT"] = accountID
	case "codex":
		env["HOME"] = sandbox
		env["CODEX_HOME"] = sandbox
		env["AI_DESK_ACCOUNT"] = accountID
	case "opencode":
		env["HOME"] = sandbox
		env["OPENCODE_CONFIG_DIR"] = sandbox
		env["AI_DESK_ACCOUNT"] = accountID
	}
	return env, nil
}

// AttachSkill dynamically adds an extra skill to an active session sandbox at runtime.
func (v *Virtualizer) AttachSkill(tool, accountID, skillName string) error {
	if err := ValidateSkillName(skillName); err != nil {
		return err
	}
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
	if err := ValidateSkillName(skillName); err != nil {
		return err
	}
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

// projectRTK projects global RTK (Rust Token Killer) hooks and configurations
// into the isolated harness sandbox, ensuring transparent terminal token reduction.
func (v *Virtualizer) projectRTK(tool, sandbox string) {
	home, _ := configpaths.HomeDir()
	switch tool {
	case "claude":
		srcRTK := filepath.Join(home, ".claude", "RTK.md")
		if _, err := os.Stat(srcRTK); err == nil {
			v.ensureSymlink(srcRTK, filepath.Join(sandbox, "RTK.md"))
		}
		srcSettings := filepath.Join(home, ".claude", "settings.json")
		if _, err := os.Stat(srcSettings); err == nil {
			v.ensureSymlink(srcSettings, filepath.Join(sandbox, "settings.json"))
		}

	case "agy", "gemini":
		srcHooks := filepath.Join(home, ".gemini", "hooks")
		if _, err := os.Stat(srcHooks); err == nil {
			v.ensureSymlink(srcHooks, filepath.Join(sandbox, "hooks"))
		}
		srcMD := filepath.Join(home, ".gemini", "GEMINI.md")
		if _, err := os.Stat(srcMD); err == nil {
			v.ensureSymlink(srcMD, filepath.Join(sandbox, "GEMINI.md"))
		}
		srcSettings := filepath.Join(home, ".gemini", "settings.json")
		if _, err := os.Stat(srcSettings); err == nil {
			v.ensureSymlink(srcSettings, filepath.Join(sandbox, "settings.json"))
		}

	case "codex":
		srcHooks := filepath.Join(home, ".codex", "hooks.json")
		if _, err := os.Stat(srcHooks); err == nil {
			v.ensureSymlink(srcHooks, filepath.Join(sandbox, "hooks.json"))
		}
		srcRTK := filepath.Join(home, ".codex", "RTK.md")
		if _, err := os.Stat(srcRTK); err == nil {
			v.ensureSymlink(srcRTK, filepath.Join(sandbox, "RTK.md"))
		}

	case "opencode":
		srcPlugins := filepath.Join(home, ".config", "opencode", "plugins")
		if _, err := os.Stat(srcPlugins); err == nil {
			v.ensureSymlink(srcPlugins, filepath.Join(sandbox, "plugins"))
		}
	}
}

// projectHostDotfiles symlinks user dotfiles into the sandbox so standard
// shell and git development commands continue functioning when HOME=sandbox.
func (v *Virtualizer) projectHostDotfiles(sandbox string) {
	home, err := configpaths.HomeDir()
	if err != nil || home == "" || sandbox == home {
		return
	}

	dotfiles := []string{
		".gitconfig",
		".ssh",
		".bashrc",
		".zshrc",
		".profile",
		".local",
		".claude.json",
	}

	for _, df := range dotfiles {
		src := filepath.Join(home, df)
		if _, err := os.Stat(src); err == nil {
			v.ensureSymlink(src, filepath.Join(sandbox, df))
		}
	}
}

// projectCredentials projects host credentials and configuration into the sandbox
// so agents never ask for re-login when switching contexts or harnesses.
func (v *Virtualizer) projectCredentials(tool, accountID, sandbox string) {
	home, err := configpaths.HomeDir()
	if err != nil || home == "" || sandbox == home {
		return
	}

	switch tool {
	case "claude":
		// 1. ~/.claude.json in sandbox root
		claudeJSON := filepath.Join(home, ".claude.json")
		if _, err := os.Stat(claudeJSON); err == nil {
			v.ensureSymlink(claudeJSON, filepath.Join(sandbox, ".claude.json"))
		}
		// 2. ~/.claude/.credentials.json, settings.json, mcp auth in sandbox and sandbox/.claude
		claudeDir := filepath.Join(sandbox, ".claude")
		_ = os.MkdirAll(claudeDir, 0700)
		hostClaude := filepath.Join(home, ".claude")
		for _, f := range []string{".credentials.json", "mcp-needs-auth-cache.json", "settings.json"} {
			if f == ".credentials.json" && accountID != "default" && accountID != "" {
				continue
			}
			src := filepath.Join(hostClaude, f)
			if _, err := os.Stat(src); err == nil {
				v.ensureSymlink(src, filepath.Join(sandbox, f))
				v.ensureSymlink(src, filepath.Join(claudeDir, f))
			}
		}

	case "codex":
		codexDir := filepath.Join(sandbox, ".codex")
		_ = os.MkdirAll(codexDir, 0700)
		hostCodex := filepath.Join(home, ".codex")
		files := []string{"auth.json", "config.toml", "installation_id", "version.json"}
		for _, f := range files {
			if f == "auth.json" && accountID != "default" && accountID != "" {
				continue
			}
			src := filepath.Join(hostCodex, f)
			if _, err := os.Stat(src); err == nil {
				v.ensureSymlink(src, filepath.Join(sandbox, f))
				v.ensureSymlink(src, filepath.Join(codexDir, f))
			}
		}

	case "opencode":
		hostOpenCode := filepath.Join(home, ".config", "opencode")
		sandboxConfig := filepath.Join(sandbox, ".config", "opencode")
		_ = os.MkdirAll(sandboxConfig, 0700)
		if entries, err := os.ReadDir(hostOpenCode); err == nil {
			for _, e := range entries {
				if e.Name() == "skills" || e.Name() == "plugins" {
					continue
				}
				src := filepath.Join(hostOpenCode, e.Name())
				v.ensureSymlink(src, filepath.Join(sandboxConfig, e.Name()))
			}
		}

	case "agy", "gemini":
		geminiDir := filepath.Join(sandbox, ".gemini")
		cliDir := filepath.Join(geminiDir, "antigravity-cli")
		_ = os.MkdirAll(cliDir, 0700)
		hostGemini := filepath.Join(home, ".gemini")

		// Link oauth credentials from host if not present
		credsPath := filepath.Join(geminiDir, "oauth_creds.json")
		if _, err := os.Stat(credsPath); os.IsNotExist(err) {
			hostCreds := filepath.Join(hostGemini, "oauth_creds.json")
			if _, err := os.Stat(hostCreds); err == nil {
				v.ensureSymlink(hostCreds, credsPath)
			}
		}
		jetskiPath := filepath.Join(geminiDir, "jetski-standalone-oauth-token")
		if _, err := os.Stat(jetskiPath); os.IsNotExist(err) {
			hostJetski := filepath.Join(hostGemini, "jetski-standalone-oauth-token")
			if _, err := os.Stat(hostJetski); err == nil {
				v.ensureSymlink(hostJetski, jetskiPath)
			}
		}
		cliOauth := filepath.Join(cliDir, "antigravity-oauth-token")
		if _, err := os.Stat(cliOauth); os.IsNotExist(err) {
			hostCliOauth := filepath.Join(hostGemini, "antigravity-cli", "antigravity-oauth-token")
			if _, err := os.Stat(hostCliOauth); err == nil {
				v.ensureSymlink(hostCliOauth, cliOauth)
			}
		}

		// Ensure legacy sandbox/antigravity-cli has access to token
		legacyCli := filepath.Join(sandbox, "antigravity-cli")
		_ = os.MkdirAll(legacyCli, 0700)
		if _, err := os.Stat(cliOauth); err == nil {
			v.ensureSymlink(cliOauth, filepath.Join(legacyCli, "antigravity-oauth-token"))
		}

		// Ensure google_accounts.json is not invalid/default
		gaPath := filepath.Join(geminiDir, "google_accounts.json")
		replaceGA := false
		if data, err := os.ReadFile(gaPath); err == nil {
			if strings.Contains(string(data), `"active": "default"`) || strings.Contains(string(data), `"active":"default"`) {
				replaceGA = true
			}
		} else {
			replaceGA = true
		}
		if replaceGA {
			hostGA := filepath.Join(hostGemini, "google_accounts.json")
			if data, err := os.ReadFile(hostGA); err == nil {
				_ = os.Remove(gaPath)
				_ = os.WriteFile(gaPath, data, 0600)
			}
		}
	}
}

// PrepareAgyAccount sets up a fully isolated ~/.gemini configuration tree inside
// the sandbox for a specific Google Antigravity account, preventing token clashing.
func (v *Virtualizer) PrepareAgyAccount(accountID, tokenJSON, email string) error {
	sandbox := v.SandboxPath("agy", accountID)
	geminiDir := filepath.Join(sandbox, ".gemini")
	cliDir := filepath.Join(geminiDir, "antigravity-cli")
	if err := os.MkdirAll(cliDir, 0700); err != nil {
		return fmt.Errorf("create gemini cli dir: %w", err)
	}

	// Symlink host dotfiles
	v.projectHostDotfiles(sandbox)

	home, _ := configpaths.HomeDir()
	hostGemini := ""
	if home != "" {
		hostGemini = filepath.Join(home, ".gemini")
	}

	// Fallback to host token if tokenJSON is empty
	if tokenJSON == "" && hostGemini != "" && (accountID == "" || accountID == "default") {
		if data, err := os.ReadFile(filepath.Join(hostGemini, "jetski-standalone-oauth-token")); err == nil && len(data) > 0 {
			tokenJSON = string(data)
		} else if data, err := os.ReadFile(filepath.Join(hostGemini, "oauth_creds.json")); err == nil && len(data) > 0 {
			tokenJSON = string(data)
		} else if data, err := os.ReadFile(filepath.Join(hostGemini, "antigravity-cli", "antigravity-oauth-token")); err == nil && len(data) > 0 {
			tokenJSON = string(data)
		}
	}

	// Resolve email if empty or "default"
	if (email == "" || email == "default") && hostGemini != "" {
		if data, err := os.ReadFile(filepath.Join(hostGemini, "google_accounts.json")); err == nil {
			var parsed map[string]any
			if json.Unmarshal(data, &parsed) == nil {
				if act, ok := parsed["active"].(string); ok && act != "" && act != "default" {
					email = act
				}
			}
		}
	}

	if tokenJSON != "" {
		// 1. Write antigravity-oauth-token
		oauthTokenPath := filepath.Join(cliDir, "antigravity-oauth-token")
		_ = os.Remove(oauthTokenPath)
		if err := os.WriteFile(oauthTokenPath, []byte(tokenJSON), 0600); err != nil {
			return fmt.Errorf("write antigravity-oauth-token: %w", err)
		}

		// Also mirror into legacy sandbox/antigravity-cli
		legacyCli := filepath.Join(sandbox, "antigravity-cli")
		_ = os.MkdirAll(legacyCli, 0700)
		legacyTokenPath := filepath.Join(legacyCli, "antigravity-oauth-token")
		_ = os.Remove(legacyTokenPath)
		v.ensureSymlink(oauthTokenPath, legacyTokenPath)

		// 2. Write jetski-standalone-oauth-token
		jetskiTokenPath := filepath.Join(geminiDir, "jetski-standalone-oauth-token")
		_ = os.Remove(jetskiTokenPath)
		if err := os.WriteFile(jetskiTokenPath, []byte(tokenJSON), 0600); err != nil {
			return fmt.Errorf("write jetski-standalone-oauth-token: %w", err)
		}

		// 3. Construct and write oauth_creds.json
		var parsed map[string]any
		if err := json.Unmarshal([]byte(tokenJSON), &parsed); err == nil {
			accessToken := ""
			refreshToken := ""
			idToken := ""
			tokenType := "Bearer"
			scope := "https://www.googleapis.com/auth/cloud-platform openid https://www.googleapis.com/auth/userinfo.email"
			var expiryDate any = 0

			// Extract from inner token object if present
			if innerRaw, ok := parsed["token"]; ok {
				if inner, isMap := innerRaw.(map[string]any); isMap {
					if at, ok := inner["access_token"].(string); ok {
						accessToken = at
					}
					if rt, ok := inner["refresh_token"].(string); ok {
						refreshToken = rt
					}
					if it, ok := inner["id_token"].(string); ok {
						idToken = it
					}
					if tt, ok := inner["token_type"].(string); ok && tt != "" {
						tokenType = tt
					}
					if sc, ok := inner["scope"].(string); ok && sc != "" {
						scope = sc
					}
					if exp, ok := inner["expiry_date"]; ok {
						expiryDate = exp
					}
				}
			} else {
				if at, ok := parsed["access_token"].(string); ok {
					accessToken = at
				}
				if rt, ok := parsed["refresh_token"].(string); ok {
					refreshToken = rt
				}
				if it, ok := parsed["id_token"].(string); ok {
					idToken = it
				}
			}

			creds := map[string]any{
				"access_token":  accessToken,
				"refresh_token": refreshToken,
				"id_token":      idToken,
				"token_type":    tokenType,
				"scope":         scope,
				"expiry_date":   expiryDate,
			}
			credsBytes, _ := json.MarshalIndent(creds, "", "  ")
			credsPath := filepath.Join(geminiDir, "oauth_creds.json")
			_ = os.Remove(credsPath)
			_ = os.WriteFile(credsPath, credsBytes, 0600)
		}
	}

	// 4. Write google_accounts.json
	gaPath := filepath.Join(geminiDir, "google_accounts.json")
	if email != "" && email != "default" {
		ga := map[string]any{
			"active": email,
			"old":    []any{},
		}
		gaBytes, _ := json.MarshalIndent(ga, "", "  ")
		_ = os.Remove(gaPath)
		_ = os.WriteFile(gaPath, gaBytes, 0600)
	} else if hostGemini != "" {
		if data, err := os.ReadFile(filepath.Join(hostGemini, "google_accounts.json")); err == nil {
			_ = os.Remove(gaPath)
			_ = os.WriteFile(gaPath, data, 0600)
		}
	}

	// 5. Copy host settings.json and installation_id if present
	if home != "" {
		hostSettings := filepath.Join(home, ".gemini", "settings.json")
		if data, err := os.ReadFile(hostSettings); err == nil {
			_ = os.WriteFile(filepath.Join(geminiDir, "settings.json"), data, 0600)
		}
		hostInstallID := filepath.Join(home, ".gemini", "installation_id")
		if data, err := os.ReadFile(hostInstallID); err == nil {
			_ = os.WriteFile(filepath.Join(geminiDir, "installation_id"), data, 0644)
		}

		// 6. Copy trustedFolders.json so user is never prompted for folder trust
		hostTrusted := filepath.Join(home, ".gemini", "trustedFolders.json")
		if data, err := os.ReadFile(hostTrusted); err == nil {
			_ = os.WriteFile(filepath.Join(geminiDir, "trustedFolders.json"), data, 0600)
			_ = os.WriteFile(filepath.Join(cliDir, "trustedFolders.json"), data, 0600)
		}
	}

	return nil
}

// TrustProjectFolder ensures projectPath is marked as TRUST_FOLDER in all host and sandbox trustedFolders.json files.
func (v *Virtualizer) TrustProjectFolder(projectPath string) error {
	if projectPath == "" {
		return nil
	}
	absPath, err := filepath.Abs(projectPath)
	if err == nil {
		projectPath = absPath
	}

	home, _ := configpaths.HomeDir()
	var targetFiles []string
	if home != "" {
		targetFiles = append(targetFiles,
			filepath.Join(home, ".gemini", "trustedFolders.json"),
			filepath.Join(home, ".gemini", "antigravity-cli", "trustedFolders.json"),
		)
	}

	// Also target all existing agy sandbox directories
	sandboxesDir := filepath.Join(v.baseDir, "agy")
	if entries, err := os.ReadDir(sandboxesDir); err == nil {
		for _, e := range entries {
			if e.IsDir() {
				targetFiles = append(targetFiles,
					filepath.Join(sandboxesDir, e.Name(), ".gemini", "trustedFolders.json"),
					filepath.Join(sandboxesDir, e.Name(), ".gemini", "antigravity-cli", "trustedFolders.json"),
				)
			}
		}
	}

	for _, f := range targetFiles {
		_ = os.MkdirAll(filepath.Dir(f), 0700)
		folders := make(map[string]string)
		if data, err := os.ReadFile(f); err == nil && len(data) > 0 {
			_ = json.Unmarshal(data, &folders)
		}
		if folders[projectPath] != "TRUST_FOLDER" {
			folders[projectPath] = "TRUST_FOLDER"
			if updated, err := json.MarshalIndent(folders, "", "  "); err == nil {
				_ = os.WriteFile(f, updated, 0600)
			}
		}
	}
	return nil
}

// ReplicateToolPermissions ensures all tool settings, folder trust, and hooks are replicated
// from host to sandbox and across accounts for the specified harness tool and account.
func (v *Virtualizer) ReplicateToolPermissions(tool, accountID, projectPath string) error {
	if projectPath != "" {
		_ = v.TrustProjectFolder(projectPath)
	}
	sandbox := v.SandboxPath(tool, accountID)
	home, _ := configpaths.HomeDir()
	if home == "" || sandbox == home {
		return nil
	}

	switch strings.ToLower(tool) {
	case "agy", "gemini":
		geminiDir := filepath.Join(sandbox, ".gemini")
		cliDir := filepath.Join(geminiDir, "antigravity-cli")
		_ = os.MkdirAll(cliDir, 0700)

		// Mirror trustedFolders.json
		hostTF := filepath.Join(home, ".gemini", "trustedFolders.json")
		if data, err := os.ReadFile(hostTF); err == nil {
			_ = os.WriteFile(filepath.Join(geminiDir, "trustedFolders.json"), data, 0600)
			_ = os.WriteFile(filepath.Join(cliDir, "trustedFolders.json"), data, 0600)
		}
		// Mirror settings.json
		hostSettings := filepath.Join(home, ".gemini", "settings.json")
		if data, err := os.ReadFile(hostSettings); err == nil {
			_ = os.WriteFile(filepath.Join(geminiDir, "settings.json"), data, 0600)
		}

	case "claude":
		claudeDir := filepath.Join(sandbox, ".claude")
		_ = os.MkdirAll(claudeDir, 0700)
		hostClaude := filepath.Join(home, ".claude")
		for _, f := range []string{"settings.json", "mcp-needs-auth-cache.json"} {
			src := filepath.Join(hostClaude, f)
			if _, err := os.Stat(src); err == nil {
				v.ensureSymlink(src, filepath.Join(claudeDir, f))
			}
		}

	case "codex":
		codexDir := filepath.Join(sandbox, ".codex")
		_ = os.MkdirAll(codexDir, 0700)
		hostCodex := filepath.Join(home, ".codex")
		for _, f := range []string{"config.toml", "hooks.json"} {
			src := filepath.Join(hostCodex, f)
			if _, err := os.Stat(src); err == nil {
				v.ensureSymlink(src, filepath.Join(codexDir, f))
			}
		}
	}

	return nil
}

// SyncAllAccountPermissions ensures all project paths and tool configurations are synchronized
// across all discovered accounts and virtual sandboxes.
func (v *Virtualizer) SyncAllAccountPermissions(projectPaths []string) error {
	for _, p := range projectPaths {
		if p != "" {
			_ = v.TrustProjectFolder(p)
		}
	}
	tools := []string{"agy", "claude", "codex", "opencode"}
	for _, t := range tools {
		sandboxesDir := filepath.Join(v.baseDir, t)
		if entries, err := os.ReadDir(sandboxesDir); err == nil {
			for _, e := range entries {
				if e.IsDir() {
					for _, p := range projectPaths {
						_ = v.ReplicateToolPermissions(t, e.Name(), p)
					}
				}
			}
		}
	}
	return nil
}

// WriteAgyToken writes an isolated Google Antigravity credential inside the sandbox.
func (v *Virtualizer) WriteAgyToken(accountID, tokenJSON, email string) error {
	// Call full PrepareAgyAccount to ensure .gemini and oauth files exist
	if err := v.PrepareAgyAccount(accountID, tokenJSON, email); err != nil {
		return err
	}

	sandbox := v.SandboxPath("agy", accountID)
	// Write active account hint
	if email != "" {
		hintFile := filepath.Join(sandbox, "active_account.json")
		hintData, _ := json.Marshal(map[string]string{"email": email})
		_ = os.WriteFile(hintFile, hintData, 0600)
	}
	return nil
}

// ValidateSkillName prevents selections from escaping the skill directory.
func ValidateSkillName(name string) error {
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, "/\\") || filepath.IsAbs(name) {
		return fmt.Errorf("invalid skill name %q", name)
	}
	return nil
}

// WithSkillsPath changes only the catalog source, retaining session isolation.
func (v *Virtualizer) WithSkillsPath(path string) *Virtualizer {
	clone := *v
	clone.skillsPath = path
	return &clone
}
