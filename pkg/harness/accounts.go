package harness

import (
	"encoding/json"
	"fmt"
	"github.com/falluba/ai-desk/pkg/configpaths"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/swapbridge"
	"os"
	"path/filepath"
	"sort"
)

// AccountsPath contains provider-specific credentials; never a shared global login.
func AccountsPath() string {
	if p := os.Getenv("AI_DESK_ACCOUNTS_DIR"); p != "" {
		return p
	}
	home, _ := configpaths.HomeDir()
	return filepath.Join(home, ".ai-desk", "accounts")
}

func credentialFile(tool string) string {
	switch tool {
	case "codex":
		return "auth.json"
	case "claude":
		return ".credentials.json"
	}
	return ""
}

func ListAccounts(tool string) ([]rotation.AccountStatus, error) {
	if tool == "agy" || tool == "gemini" {
		return swapbridge.NewSwapBridge("").LoadAccounts()
	}
	if credentialFile(tool) == "" {
		return nil, fmt.Errorf("account switching is not supported for %s", tool)
	}
	entries, err := os.ReadDir(filepath.Join(AccountsPath(), tool))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var accounts []rotation.AccountStatus
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(AccountsPath(), tool, entry.Name(), credentialFile(tool))); err != nil {
			continue
		}
		accounts = append(accounts, rotation.AccountStatus{ID: entry.Name(), Email: entry.Name(), Provider: tool, RemainingQuotaPct: 100})
	}
	sort.Slice(accounts, func(i, j int) bool { return accounts[i].ID < accounts[j].ID })
	return accounts, nil
}

// ImportAccount copies a credential document without exposing its contents.
func ImportAccount(tool, id, source string) error {
	if err := ValidateSkillName(id); err != nil {
		return err
	}
	filename := credentialFile(tool)
	if filename == "" {
		return fmt.Errorf("import supports codex and claude; use agy-swap for Antigravity")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	if !json.Valid(data) {
		return fmt.Errorf("credential file must contain JSON")
	}
	dir := filepath.Join(AccountsPath(), tool, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, filename), data, 0600)
}

// PrepareCredentials validates the selected identity before process replacement.
func (v *Virtualizer) PrepareCredentials(tool, id string) error {
	if id == "" || id == "default" {
		return nil
	} // explicitly use native login
	if err := ValidateSkillName(id); err != nil {
		return err
	}
	if tool == "agy" || tool == "gemini" {
		token, err := swapbridge.NewSwapBridge("").ReadAccountToken(id)
		if err != nil {
			return err
		}
		if token == "" {
			return fmt.Errorf("no Antigravity credentials for %q", id)
		}
		return v.PrepareAgyAccount(id, token, id)
	}
	filename := credentialFile(tool)
	if filename == "" {
		return fmt.Errorf("account switching is not supported for %s", tool)
	}
	source := filepath.Join(AccountsPath(), tool, id, filename)
	data, err := os.ReadFile(source)
	if err != nil {
		return fmt.Errorf("credentials for %s account %q: %w", tool, id, err)
	}
	if !json.Valid(data) {
		return fmt.Errorf("invalid credentials for %s account %q", tool, id)
	}
	dir := v.SandboxPath(tool, id)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	target := filepath.Join(dir, filename)
	// Replace only this sandbox's credential entry; do not follow an old host symlink.
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return os.WriteFile(target, data, 0600)
}
