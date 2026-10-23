package deck

import (
	"encoding/json"
	"fmt"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"os"
	"path/filepath"
)

// SessionConfig is stored alongside upstream tool_data without removing its keys.
type SessionConfig struct {
	Version        int      `json:"config_version"`
	AccountProfile string   `json:"account_profile"`
	SkillProfile   string   `json:"skill_profile"`
	SelectedSkills []string `json:"selected_skills"`
	SkillsPending  bool     `json:"skills_pending"`
}

func ReadSessionConfig(inst *InstanceRecord) (SessionConfig, error) {
	var cfg SessionConfig
	if inst.ToolData != "" {
		if err := json.Unmarshal([]byte(inst.ToolData), &cfg); err != nil {
			return cfg, fmt.Errorf("invalid session configuration: %w", err)
		}
	}
	return cfg, nil
}

func (s *StateDB) SaveSessionConfig(inst *InstanceRecord, cfg SessionConfig) error {
	data := map[string]any{}
	if inst.ToolData != "" {
		if err := json.Unmarshal([]byte(inst.ToolData), &data); err != nil {
			return err
		}
	}
	b, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	fields := map[string]any{}
	_ = json.Unmarshal(b, &fields)
	for k, v := range fields {
		data[k] = v
	}
	b, err = json.Marshal(data)
	if err != nil {
		return err
	}
	if err = s.UpdateInstanceToolData(inst.ID, string(b)); err != nil {
		return err
	}
	inst.ToolData = string(b)
	InvalidateInspectionCache(inst.ID)
	InvalidateInspectionCache("")
	return nil
}

// SessionVirtualizer keeps legacy sessions at their original location until migrated.
func SessionVirtualizer(v *harness.Virtualizer, inst *InstanceRecord) *harness.Virtualizer {
	cfg, _ := ReadSessionConfig(inst)
	if cfg.Version >= 1 {
		return v.ForSession(inst.ID)
	}
	return v
}

func SessionSkills(v *harness.Virtualizer, inst *InstanceRecord) ([]string, error) {
	cfg, err := ReadSessionConfig(inst)
	if err != nil {
		return nil, err
	}
	if cfg.Version >= 1 {
		return cfg.SelectedSkills, nil
	}
	return v.ListAttachedSkills(inst.Tool, inst.Account)
}

// SetSessionSkills stages changes; no claim is made that the running CLI reloaded them.
func (s *StateDB) SetSessionSkills(v *harness.Virtualizer, id, profile string, skills []string) error {
	inst, err := s.FindInstance(id)
	if err != nil {
		return err
	}
	for _, name := range skills {
		if err := harness.ValidateSkillName(name); err != nil {
			return err
		}
		if info, err := os.Stat(filepath.Join(v.SkillsPath(), name)); err != nil || !info.IsDir() {
			return fmt.Errorf("skill %q is not installed", name)
		}
	}
	cfg, err := ReadSessionConfig(inst)
	if err != nil {
		return err
	}
	if cfg.AccountProfile == "" {
		profiles, err := rotation.LoadAccountProfiles("")
		if err != nil {
			return err
		}
		cfg.AccountProfile = profiles.ActiveProfile
	}
	cfg.Version = 1
	cfg.SkillProfile = profile
	cfg.SelectedSkills = append([]string{}, skills...)
	cfg.SkillsPending = true
	return s.SaveSessionConfig(inst, cfg)
}

func (s *StateDB) SetAccountProfile(id, profile string) error {
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return err
	}
	if _, ok := profiles.Profiles[profile]; !ok {
		return fmt.Errorf("unknown account profile %q", profile)
	}
	inst, err := s.FindInstance(id)
	if err != nil {
		return err
	}
	cfg, err := ReadSessionConfig(inst)
	if err != nil {
		return err
	}
	cfg.AccountProfile = profile
	return s.SaveSessionConfig(inst, cfg)
}

// SnapshotAccountProfiles prevents later global-default changes from rerouting old sessions.
func (s *StateDB) SnapshotAccountProfiles() error {
	var count int
	if err := s.db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='instances'").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return err
	}
	instances, err := s.LoadInstances()
	if err != nil {
		return err
	}
	for _, inst := range instances {
		cfg, err := ReadSessionConfig(&inst)
		if err != nil {
			return err
		}
		if cfg.AccountProfile != "" {
			continue
		}
		cfg.AccountProfile = profiles.ActiveProfile
		if err = s.SaveSessionConfig(&inst, cfg); err != nil {
			return err
		}
	}
	return nil
}
