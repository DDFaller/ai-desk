package harness

import (
	"bufio"
	"fmt"
	"github.com/falluba/ai-desk/pkg/configpaths"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type ProfileDefinition struct {
	Description string   `yaml:"description"`
	Skills      []string `yaml:"skills"`
}

type SkillProfilesConfig struct {
	Profiles map[string]ProfileDefinition `yaml:"profiles"`
}

type SkillDetail struct {
	Name        string
	Category    string
	Description string
	WordCount   int
}

type SkillAuditReport struct {
	TotalSkills     int
	TotalWords      int
	CatalogTokenTax int
	CategoryCounts  map[string]int
	Skills          []SkillDetail
}

// DefaultSkillProfilesPath returns ~/.ai-desk/skill-profiles.yaml
func DefaultSkillProfilesPath() string {
	home, _ := configpaths.HomeDir()
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
	if cfg.Profiles == nil {
		cfg.Profiles = make(map[string]ProfileDefinition)
	}
	cfg.Profiles["none"] = ProfileDefinition{Description: "No skills injected; browse and attach manually", Skills: []string{}}
	return cfg, nil
}

func defaultProfilesConfig() *SkillProfilesConfig {
	return &SkillProfilesConfig{
		Profiles: map[string]ProfileDefinition{
			"none": {Description: "No skills injected; browse and attach manually", Skills: []string{}},
			"minimal": {
				Description: "Lean baseline for pure coding without specialized framework tools",
				Skills:      []string{"verification", "react-best-practices"},
			},
			"core": {
				Description: "Standard developer suite",
				Skills:      []string{"verification", "react-best-practices", "shadcn", "antigravity-guide"},
			},
			"web": {
				Description: "Vercel, Next.js, and modern fullstack cloud engineering",
				Skills: []string{
					"nextjs", "react-best-practices", "shadcn", "ai-sdk", "ai-gateway",
					"auth", "cdn-caching", "deployments-cicd", "env-vars", "microfrontends",
					"next-cache-components", "routing-middleware", "runtime-cache", "turbopack",
					"verification", "workflow",
				},
			},
			"video": {
				Description: "HyperFrames creative direction, animation, keyframes, and audio",
				Skills: []string{
					"hyperframes", "hyperframes-animation", "hyperframes-audio", "hyperframes-cli",
					"hyperframes-core", "hyperframes-creative", "hyperframes-keyframes", "hyperframes-registry",
					"hyperframes-studio", "media-use", "product-launch-video", "vox-video-studio", "general-video",
				},
			},
			"backend": {
				Description: "Backend API and database engineering",
				Skills:      []string{"verification", "auth", "env-vars", "deployments-cicd", "bootstrap"},
			},
			"all": {
				Description: "Full unrestricted catalog of all 76 skills",
				Skills:      []string{"*"},
			},
		},
	}
}

// ResolveSkills resolves the list of skill directory names for a given profile or comma-separated list of profiles.
func (cfg *SkillProfilesConfig) ResolveSkills(profileName, canonicalSkillsDir string) ([]string, error) {
	if profileName == "none" {
		return []string{}, nil
	}
	if profileName == "" || profileName == "auto" {
		profileName = DetectWorkspaceProfile("")
	}

	// Support comma-separated profiles (e.g. "core,web" or "core, video")
	if strings.Contains(profileName, ",") {
		parts := strings.Split(profileName, ",")
		seen := make(map[string]bool)
		var combined []string
		for _, part := range parts {
			part = strings.TrimSpace(part)
			if part == "" {
				continue
			}
			skills, err := cfg.ResolveSkills(part, canonicalSkillsDir)
			if err != nil {
				return nil, err
			}
			for _, s := range skills {
				if !seen[s] {
					seen[s] = true
					combined = append(combined, s)
				}
			}
		}
		return combined, nil
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

// DetectWorkspaceProfile inspects the workspace directory and infers the best profile.
func DetectWorkspaceProfile(workdir string) string {
	if workdir == "" {
		workdir, _ = os.Getwd()
	}

	// Web / Next.js signatures
	for _, f := range []string{"package.json", "next.config.js", "next.config.mjs", "next.config.ts"} {
		if _, err := os.Stat(filepath.Join(workdir, f)); err == nil {
			return "web"
		}
	}

	// Video / HyperFrames signatures
	for _, f := range []string{"hyperframes.json", "BRIEF.md", "remotion.config.ts"} {
		if _, err := os.Stat(filepath.Join(workdir, f)); err == nil {
			return "video"
		}
	}

	// Career / Job search signatures
	for _, f := range []string{"job.json", "curriculum", "source.md"} {
		if _, err := os.Stat(filepath.Join(workdir, f)); err == nil {
			return "career"
		}
	}

	return "core"
}

// ListAllSkillNames reads canonical skills directory and returns all subfolder names.
func ListAllSkillNames(canonicalSkillsDir string) ([]string, error) {
	if canonicalSkillsDir == "" {
		home, _ := configpaths.HomeDir()
		canonicalSkillsDir = filepath.Join(home, ".agents", "skills")
	}

	entries, err := os.ReadDir(canonicalSkillsDir)
	if err != nil {
		return nil, fmt.Errorf("read canonical skills %s: %w", canonicalSkillsDir, err)
	}

	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		// Follow symlinks so linked skills (e.g. job-application-agents) are included
		fullPath := filepath.Join(canonicalSkillsDir, e.Name())
		info, err := os.Stat(fullPath)
		if err == nil && info.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	return names, nil
}

// AuditSkills performs an in-depth audit of word count and prompt tax across canonical skills.
func AuditSkills(canonicalSkillsDir string) (*SkillAuditReport, error) {
	if canonicalSkillsDir == "" {
		home, _ := configpaths.HomeDir()
		canonicalSkillsDir = filepath.Join(home, ".agents", "skills")
	}

	names, err := ListAllSkillNames(canonicalSkillsDir)
	if err != nil {
		return nil, err
	}

	report := &SkillAuditReport{
		TotalSkills:    len(names),
		CategoryCounts: make(map[string]int),
		Skills:         make([]SkillDetail, 0, len(names)),
	}

	for _, name := range names {
		skillFile := filepath.Join(canonicalSkillsDir, name, "SKILL.md")
		data, err := os.ReadFile(skillFile)
		if err != nil {
			continue
		}

		desc := extractDescription(string(data))
		wordCount := len(strings.Fields(string(data)))
		category := categorizeSkill(name)

		report.TotalWords += wordCount
		report.CategoryCounts[category]++
		report.Skills = append(report.Skills, SkillDetail{
			Name:        name,
			Category:    category,
			Description: desc,
			WordCount:   wordCount,
		})
	}

	// Description tax is ~60 tokens per skill
	report.CatalogTokenTax = report.TotalSkills * 60
	return report, nil
}

func categorizeSkill(name string) string {
	lower := strings.ToLower(name)
	switch {
	case strings.Contains(lower, "hyperframes") || strings.Contains(lower, "video") || lower == "media-use":
		return "video"
	case strings.Contains(lower, "job") || strings.Contains(lower, "application") || strings.Contains(lower, "curriculum") || strings.Contains(lower, "interview") || strings.Contains(lower, "candidate") || strings.Contains(lower, "latex"):
		return "career"
	case strings.Contains(lower, "next") || strings.Contains(lower, "vercel") || strings.Contains(lower, "react") || strings.Contains(lower, "shadcn") || strings.Contains(lower, "auth") || strings.Contains(lower, "routing") || strings.Contains(lower, "cache") || strings.Contains(lower, "deployments"):
		return "web"
	case strings.Contains(lower, "signal"):
		return "signal"
	default:
		return "core"
	}
}

func extractDescription(content string) string {
	scanner := bufio.NewScanner(strings.NewReader(content))
	inFrontmatter := false
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "---" {
			if !inFrontmatter {
				inFrontmatter = true
				continue
			} else {
				break
			}
		}
		if inFrontmatter && strings.HasPrefix(line, "description:") {
			return strings.TrimSpace(strings.TrimPrefix(line, "description:"))
		}
	}
	return ""
}
