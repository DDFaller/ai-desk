package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

func (m Model) skillProfileOptions() []string {
	options := append([]string{}, availableSkillProfiles...)
	if m.skillsConfig == nil {
		return options
	}
	options = nil
	for _, name := range availableSkillProfiles {
		if _, ok := m.skillsConfig.Profiles[name]; ok {
			options = append(options, name)
		}
	}
	var extra []string
	for name := range m.skillsConfig.Profiles {
		found := false
		for _, n := range options {
			if name == n {
				found = true
			}
		}
		if !found {
			extra = append(extra, name)
		}
	}
	sort.Strings(extra)
	return append(options, extra...)
}

func (m Model) selectedInstance() *deck.InstanceRecord {
	if m.selectedTreeIdx >= 0 && m.selectedTreeIdx < len(m.treeItems) {
		return m.treeItems[m.selectedTreeIdx].Session
	}
	return nil
}

func (m Model) skillEntries() []string {
	names := m.skillProfileOptions()
	if m.skillCatalog {
		names, _ = harness.ListAllSkillNames(m.v.SkillsPath())
		if a := m.selectedAgent(); a != nil {
			entries, _ := os.ReadDir(filepath.Join(a.Home, "skills"))
			for _, e := range entries {
				if e.IsDir() {
					names = append(names, "agent--"+e.Name())
				}
			}
		}
	}
	var filtered []string
	for _, name := range names {
		if strings.Contains(strings.ToLower(name), strings.ToLower(m.skillQuery)) {
			filtered = append(filtered, name)
		}
	}
	return filtered
}

func (m Model) updateSkills(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	names := m.skillEntries()
	switch key {
	case "esc":
		if m.skillPreview != "" {
			m.skillPreview = ""
		} else {
			m.showSkillsOverlay = false
		}
		return m, nil
	case "tab":
		m.skillCatalog = !m.skillCatalog
		m.skillQuery = ""
		m.selectedProfile = 0
		m.skillPreview = ""
		return m, nil
	case "up":
		if m.selectedProfile > 0 {
			m.selectedProfile--
		}
		return m, nil
	case "down":
		if m.selectedProfile+1 < len(names) {
			m.selectedProfile++
		}
		return m, nil
	case "backspace":
		r := []rune(m.skillQuery)
		if len(r) > 0 {
			m.skillQuery = string(r[:len(r)-1])
		}
		m.selectedProfile = 0
		return m, nil
	case "ctrl+r":
		inst := m.selectedInstance()
		if inst == nil || m.sdb == nil {
			m.setToast("Select a session first.")
			return m, nil
		}
		_, err := deck.SwitchAccount(m.sdb, m.v, inst.ID, inst.Account, true)
		if err != nil {
			m.setToast(err.Error())
		} else {
			m.setToast("Session restarted with selected skills.")
			m.reload()
		}
		return m, nil
	case "enter", " ", "ctrl+v":
		if len(names) == 0 || m.selectedProfile >= len(names) {
			return m, nil
		}
		name := names[m.selectedProfile]
		if key == "ctrl+v" {
			if m.skillCatalog {
				data, err := os.ReadFile(m.skillSource(name))
				if err != nil {
					m.setToast(err.Error())
				} else {
					m.skillPreview = string(data)
				}
			} else if m.skillsConfig != nil {
				skills, err := m.skillsConfig.ResolveSkills(name, m.v.SkillsPath())
				if err != nil {
					m.setToast(err.Error())
				} else {
					m.skillPreview = m.skillsConfig.Profiles[name].Description + "\n" + strings.Join(skills, "\n")
				}
			}
			return m, nil
		}
		inst := m.selectedInstance()
		if inst == nil || m.sdb == nil {
			if !m.skillCatalog {
				m.newTaskSkillProfiles = map[string]bool{name: true}
				m.setToast("Default for new sessions: " + name)
			}
			return m, nil
		}
		var skills []string
		profile := name
		var err error
		if m.skillCatalog {
			profile = "custom"
			skills, err = deck.SessionSkills(m.v, inst)
			if err == nil {
				found := false
				var next []string
				for _, s := range skills {
					if s == name {
						found = true
					} else {
						next = append(next, s)
					}
				}
				if !found {
					next = append(next, name)
				}
				skills = next
			}
		} else {
			skills, err = m.skillsConfig.ResolveSkills(name, m.v.SkillsPath())
		}
		if err == nil {
			err = m.sdb.SetSessionSkills(m.v, inst.ID, profile, skills)
		}
		if err != nil {
			m.setToast(err.Error())
		} else {
			m.setToast("Selection saved — restart required (Ctrl+R).")
			m.reload()
		}
		return m, nil
	default:
		if len(msg.Runes) > 0 {
			m.skillQuery += string(msg.Runes)
			m.selectedProfile = 0
		}
	}
	return m, nil
}

func (m Model) skillsView(width, height int) string {
	mode := "Profiles"
	if m.skillCatalog {
		mode = "Catalog"
	}
	lines := []string{"Skills / " + mode, "Search: " + m.skillQuery, "Tab: profiles/catalog • Enter: select/toggle • Ctrl+V: preview", "Ctrl+R: restart selected session • Esc: close", "Browsing does not load skills into the agent context."}
	if inst := m.selectedInstance(); inst != nil {
		cfg, _ := deck.ReadSessionConfig(inst)
		state := "applied"
		if cfg.SkillsPending {
			state = "restart required"
		}
		lines = append(lines, fmt.Sprintf("%s — %s (%s)", inst.Title, cfg.SkillProfile, state))
	}
	if m.skillPreview != "" {
		lines = append(lines, strings.Split(m.skillPreview, "\n")...)
	} else {
		names := m.skillEntries()
		count := max(1, height-9)
		start := max(0, m.selectedProfile-count+1)
		for i := start; i < len(names) && i < start+count; i++ {
			prefix := "  "
			if i == m.selectedProfile {
				prefix = "> "
			}
			lines = append(lines, prefix+names[i])
		}
	}
	if len(lines) > max(1, height-2) {
		lines = lines[:max(1, height-2)]
	}
	for i := range lines {
		lines[i] = truncateString(lines[i], max(1, width-4))
	}
	return activeBorder.Width(max(1, width)).Height(max(1, height)).Render(strings.Join(lines, "\n"))
}

func (m Model) skillSource(name string) string {
	if a := m.selectedAgent(); a != nil && strings.HasPrefix(name, "agent--") {
		return filepath.Join(a.Home, "skills", strings.TrimPrefix(name, "agent--"), "SKILL.md")
	}
	return filepath.Join(m.v.SkillsPath(), name, "SKILL.md")
}
