package ui

import (
	"fmt"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"sort"
)

func nextProfile(current string, cfg *rotation.AccountProfilesConfig) string {
	var names []string
	for name := range cfg.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	if len(names) == 0 {
		return ""
	}
	for i, name := range names {
		if name == current {
			return names[(i+1)%len(names)]
		}
	}
	return names[0]
}

func (m *Model) changeAccountProfile(key string) {
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		m.setToast(err.Error())
		return
	}
	if m.accountTargetInst == nil || m.sdb == nil {
		return
	}
	cfg, err := deck.ReadSessionConfig(m.accountTargetInst)
	if err != nil {
		m.setToast(err.Error())
		return
	}
	name := cfg.AccountProfile
	if name == "" {
		name = profiles.ActiveProfile
	}
	if key == "p" {
		name = nextProfile(name, profiles)
		err = m.sdb.SetAccountProfile(m.accountTargetInst.ID, name)
	} else {
		p, ok := profiles.Profiles[name]
		if !ok {
			m.setToast("Unknown profile: " + name)
			return
		}
		if key == "f" {
			p.AutoFailover = !p.AutoFailover
		} else {
			switch p.Policy {
			case rotation.PolicySticky:
				p.Policy = rotation.PolicyBalanced
			case rotation.PolicyBalanced:
				p.Policy = rotation.PolicyRoundRobin
			default:
				p.Policy = rotation.PolicySticky
			}
		}
		profiles.Profiles[name] = p
		err = profiles.Save("")
	}
	if err != nil {
		m.setToast(err.Error())
		return
	}
	m.accountTargetInst, _ = m.sdb.FindInstance(m.accountTargetInst.ID)
	m.setToast("Account profile saved: " + name)
}

func (m Model) accountProfileSummary() string {
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return err.Error()
	}
	name := profiles.ActiveProfile
	if m.accountTargetInst != nil {
		cfg, _ := deck.ReadSessionConfig(m.accountTargetInst)
		if cfg.AccountProfile != "" {
			name = cfg.AccountProfile
		}
	}
	p, ok := profiles.Profiles[name]
	if !ok {
		return "Unknown profile: " + name
	}
	return fmt.Sprintf("Profile: %s | %s | auto: %t\np: profile • f: profile auto-failover • b: profile policy\nAllowed accounts: %v (empty = all compatible)", name, p.Policy, p.AutoFailover, p.Accounts)
}

func (m Model) newTaskAccounts() []rotation.AccountStatus {
	tool := "agy"
	if m.newTaskToolIdx >= 0 && m.newTaskToolIdx < len(availableHarnesses) {
		tool = availableHarnesses[m.newTaskToolIdx].ID
	}
	accounts, _ := harness.ListAccounts(tool)
	return append([]rotation.AccountStatus{{ID: "default", Email: "default", Provider: tool}}, accounts...)
}
