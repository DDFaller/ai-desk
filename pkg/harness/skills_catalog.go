package harness

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

type ProfileDefinition struct {
	Description string   `yaml:"description"`
	Skills      []string `yaml:"skills"`
}

type SkillProfilesConfig struct {
	Profiles map[string]ProfileDefinition `yaml:"profiles"`
}

type SkillInfo struct {
	Name        string
	Description string
	WordCount   int
}

// DefaultSkillProfilesPath returns ~/.ai-desk/skill-profiles.yaml
func DefaultSkillProfilesPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".ai-desk", "skill-profiles.yaml")
}

// LoadSkillProfiles loads the profiles configuration from disk.
func LoadSkillProfiles(path string) (*SkillProfilesConfig, error) {
	if path == "" {
		path = DefaultSkillProfilesPath()
	}
	cfg := &SkillProfilesConfig{
		Profiles: make(map[string]ProfileDefinition),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultProfilesConfig(), nil
		}
		return nil, fmt.Errorf("read skill-profiles.yaml: %w", err)
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse skill-profiles.yaml: %w", err)
	}
	return cfg, nil
}

func defaultProfilesConfig() *SkillProfilesConfig {
	return &SkillProfilesConfig{
		Profiles: map[string]ProfileDefinition{
			"minimal": {
				Description: "Lean baseline for pure coding without specialized framework tools",
				Skills:      []string{"verification", "react-best-practices"},
			},
			"core": {
				Description: "Standard developer suite",
				Skills:      []string{"verification", "react-best-practices", "shadcn", "antigravity-guide"},
			},
			"all": {
				Description: "Full unrestricted catalog of all 76 skills",
				Skills:      []string{"*"},
			},
		},
	}
}

// ResolveSkills resolves the list of skill directory names for a given profile.
func (cfg *SkillProfilesConfig) ResolveSkills(profileName, canonicalSkillsDir string) ([]string, error) {
	if profileName == "" {
		profileName = "core"
	}

	p, ok := cfg.Profiles[profileName]
	if !ok {
		return nil, fmt.Errorf("skill profile %q not found in skill-profiles.yaml", profileName)
	}

	// Check if wildcard "*"
	for _, s := range p.Skills {
		if s == "*" {
			return ListAllSkillNames(canonicalSkillsDir)
		}
	}

	return p.Skills, nil
}

// ListAllSkillNames reads canonical skills directory and returns all subfolder names.
func ListAllSkillNames(canonicalSkillsDir string) ([]string, error) {
	if canonicalSkillsDir == "" {
		home, _ := os.UserHomeDir()
		canonicalSkillsDir = filepath.Join(home, ".agents", "skills")
	}

	entries, err := os.ReadDir(canonicalSkillsDir)
	if err != nil {
		return nil, fmt.Errorf("read canonical skills %s: %w", canonicalSkillsDir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}
