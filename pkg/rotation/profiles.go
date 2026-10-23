package rotation

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AccountProfile defines a named profile grouping accounts with routing policy.
type AccountProfile struct {
	Name         string   `yaml:"name" json:"name"`
	Description  string   `yaml:"description" json:"description"`
	Accounts     []string `yaml:"accounts" json:"accounts"`
	Policy       Policy   `yaml:"policy" json:"policy"`
	AutoFailover bool     `yaml:"auto_failover" json:"auto_failover"`
}

// AccountProfilesConfig stores all defined account profiles.
type AccountProfilesConfig struct {
	ActiveProfile string                    `yaml:"active_profile" json:"active_profile"`
	Profiles      map[string]AccountProfile `yaml:"profiles" json:"profiles"`
}

// DefaultAccountProfilesPath returns ~/.ai-desk/account-profiles.yaml.
func DefaultAccountProfilesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-desk", "account-profiles.yaml")
}

// DefaultAccountProfilesConfig returns a sensible default configuration.
func DefaultAccountProfilesConfig() *AccountProfilesConfig {
	return &AccountProfilesConfig{
		ActiveProfile: "default",
		Profiles: map[string]AccountProfile{
			"default": {
				Name:         "default",
				Description:  "Standard balanced account rotation",
				Accounts:     nil, // all discovered accounts
				Policy:       PolicyBalanced,
				AutoFailover: true,
			},
			"work": {
				Name:         "work",
				Description:  "Dedicated primary production workloads",
				Accounts:     nil,
				Policy:       PolicySticky,
				AutoFailover: true,
			},
			"fast": {
				Name:         "fast",
				Description:  "High-throughput round-robin automation",
				Accounts:     nil,
				Policy:       PolicyRoundRobin,
				AutoFailover: true,
			},
		},
	}
}

// LoadAccountProfiles reads the profiles YAML file, creating a default one if absent.
func LoadAccountProfiles(customPath string) (*AccountProfilesConfig, error) {
	path := customPath
	if path == "" {
		path = DefaultAccountProfilesPath()
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			cfg := DefaultAccountProfilesConfig()
			_ = cfg.Save(path)
			return cfg, nil
		}
		return nil, fmt.Errorf("read account-profiles.yaml: %w", err)
	}

	var cfg AccountProfilesConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("unmarshal account-profiles.yaml: %w", err)
	}

	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]AccountProfile)
	}
	if cfg.ActiveProfile == "" {
		cfg.ActiveProfile = "default"
	}
	if _, ok := cfg.Profiles["default"]; !ok {
		cfg.Profiles["default"] = AccountProfile{
			Name:         "default",
			Description:  "Standard balanced account rotation",
			Policy:       PolicyBalanced,
			AutoFailover: true,
		}
	}

	return &cfg, nil
}

// Save writes the configuration to disk.
func (cfg *AccountProfilesConfig) Save(customPath string) error {
	path := customPath
	if path == "" {
		path = DefaultAccountProfilesPath()
	}

	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}

	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal account-profiles.yaml: %w", err)
	}

	return os.WriteFile(path, data, 0600)
}

// FilterAccounts returns candidates belonging to the specified profile.
// If profile is empty or "default" with an empty account list, returns all accounts.
func (cfg *AccountProfilesConfig) FilterAccounts(profileName string, allAccounts []AccountStatus) []AccountStatus {
	if profileName == "" {
		profileName = cfg.ActiveProfile
	}
	if profileName == "" {
		profileName = "default"
	}

	p, ok := cfg.Profiles[profileName]
	if !ok || len(p.Accounts) == 0 {
		return allAccounts
	}

	wanted := make(map[string]bool)
	for _, acc := range p.Accounts {
		wanted[strings.ToLower(strings.TrimSpace(acc))] = true
	}

	var filtered []AccountStatus
	for _, acc := range allAccounts {
		if wanted[strings.ToLower(acc.Email)] || wanted[strings.ToLower(acc.ID)] {
			filtered = append(filtered, acc)
		}
	}

	// If no match was found, fallback to all accounts to avoid starving
	if len(filtered) == 0 {
		return allAccounts
	}

	return filtered
}

// AssignAccount adds an email to a profile and saves.
func (cfg *AccountProfilesConfig) AssignAccount(profileName, email string) error {
	if profileName == "" {
		profileName = "default"
	}
	p, ok := cfg.Profiles[profileName]
	if !ok {
		p = AccountProfile{
			Name:         profileName,
			Description:  fmt.Sprintf("%s account profile", profileName),
			Policy:       PolicyBalanced,
			AutoFailover: true,
		}
	}

	email = strings.TrimSpace(email)
	for _, existing := range p.Accounts {
		if strings.EqualFold(existing, email) {
			return nil // already assigned
		}
	}

	p.Accounts = append(p.Accounts, email)
	cfg.Profiles[profileName] = p
	return nil
}
