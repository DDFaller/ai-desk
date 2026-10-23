package ui

import (
	"fmt"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
	"github.com/falluba/ai-desk/pkg/deck"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

type agentFile struct{ label, path string }

func (m Model) selectedAgent() *deck.AgentRecord {
	if m.selectedTreeIdx >= 0 && m.selectedTreeIdx < len(m.treeItems) {
		item := m.treeItems[m.selectedTreeIdx]
		if item.GroupNode != nil && item.GroupNode.Agent != nil {
			return item.GroupNode.Agent
		}
		if item.Session != nil {
			cfg, _ := deck.ReadSessionConfig(item.Session)
			for _, a := range m.agents {
				if cfg.AgentID == a.ID {
					copy := a
					return &copy
				}
			}
		}
	}
	return nil
}

func (m *Model) selectAgent(id string) {
	for i, item := range m.treeItems {
		if item.GroupNode != nil && item.GroupNode.Agent != nil && item.GroupNode.Agent.ID == id {
			m.selectedTreeIdx = i
			return
		}
	}
}

func (m Model) updateAgentManager(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	key := msg.String()
	if m.agentCreating {
		switch key {
		case "esc":
			m.agentCreating = false
		case "tab", "down":
			m.agentField = (m.agentField + 1) % len(m.agentInputs)
		case "shift+tab", "up":
			m.agentField = (m.agentField + len(m.agentInputs) - 1) % len(m.agentInputs)
		case "backspace":
			m.agentInputs[m.agentField].Backspace()
		case "ctrl+u":
			m.agentInputs[m.agentField].Clear()
		case "left":
			m.agentInputs[m.agentField].Left()
		case "right":
			m.agentInputs[m.agentField].Right()
		case "enter", "ctrl+s":
			if m.sdb == nil {
				m.setToast("Session database unavailable")
				return m, nil
			}
			values := m.agentInputs
			a, err := m.sdb.CreateAgent(deck.CreateAgentOptions{Name: values[0].Value, Role: values[1].Value, Team: values[2].Value, Repository: values[3].Value, Runtime: values[4].Value, AccountProfile: values[5].Value, SkillProfile: values[6].Value})
			if err != nil {
				m.setToast(err.Error())
			} else {
				m.agentCreating = false
				m.showAgentManager = false
				m.reload()
				m.selectAgent(a.ID)
				m.setToast("Agent created: " + a.Name)
			}
		default:
			for _, r := range msg.Runes {
				m.agentInputs[m.agentField].Insert(r)
			}
		}
		return m, nil
	}
	switch key {
	case "esc", "g":
		m.showAgentManager = false
	case "up":
		if m.agentIndex > 0 {
			m.agentIndex--
		}
	case "down":
		if m.agentIndex+1 < len(m.agents) {
			m.agentIndex++
		}
	case "n":
		m.agentCreating = true
		m.agentField = 0
		m.agentInputs = [7]TextInputState{}
		cwd, _ := os.Getwd()
		m.agentInputs[2].Set("my-agents")
		m.agentInputs[3].Set(cwd)
		m.agentInputs[4].Set("codex")
		m.agentInputs[6].Set("none")
	case "a":
		if m.sdb != nil && m.agentIndex < len(m.agents) {
			a := m.agents[m.agentIndex]
			if err := m.sdb.ArchiveAgent(a.ID, !a.Archived); err != nil {
				m.setToast(err.Error())
			} else {
				m.reload()
			}
		}
	case "enter":
		if m.agentIndex < len(m.agents) {
			a := m.agents[m.agentIndex]
			if a.Archived {
				m.setToast("Restore this agent with a first.")
			} else {
				m.showAgentManager = false
				m.selectAgent(a.ID)
			}
		}
	}
	return m, nil
}

func (m Model) agentManagerView(width, height int) string {
	lines := []string{"Persistent agents", "n: new agent • a: archive/restore • Enter: select • Esc: close"}
	if m.agentCreating {
		lines = []string{"Create a persistent agent", "Tab: next field • Enter: create • Esc: cancel"}
		labels := []string{"Name", "Role", "Team", "Local repository", "Runtime", "Account profile (blank = default)", "Skill profile"}
		for i, label := range labels {
			prefix := "  "
			if i == m.agentField {
				prefix = "> "
			}
			lines = append(lines, prefix+label+": "+m.agentInputs[i].Value)
		}
		lines = append(lines, "Creates a separate branch and worktree; sessions share that worktree.")
	} else {
		start := max(0, m.agentIndex-max(1, height-6)+1)
		for i := start; i < len(m.agents) && i < start+max(1, height-6); i++ {
			a := m.agents[i]
			prefix := "  "
			if i == m.agentIndex {
				prefix = "> "
			}
			state := ""
			if a.Archived {
				state = " [archived]"
			}
			lines = append(lines, prefix+a.Team+" / "+a.Name+" • "+a.Runtime+state)
		}
	}
	return boundedPanel(lines, width, height)
}

func boundedPanel(lines []string, width, height int) string {
	for i := range lines {
		lines[i] = truncateString(lines[i], max(1, width-4))
	}
	if len(lines) > max(1, height-2) {
		lines = lines[:max(1, height-2)]
	}
	return activeBorder.Width(max(1, width)).Height(max(1, height)).Render(strings.Join(lines, "\n"))
}

func (m *Model) loadAgentFiles() {
	m.agentFiles = nil
	m.agentFileIndex = 0
	m.agentFileContent = ""
	m.agentFileScroll = 0
	a := m.selectedAgent()
	if a == nil {
		m.setToast("Select an agent or one of its sessions.")
		return
	}
	for _, name := range []string{"AGENT.md", "USER.md", "MEMORY.md"} {
		m.agentFiles = append(m.agentFiles, agentFile{"Memory / " + name, filepath.Join(a.Home, "memory", name)})
	}
	entries, _ := os.ReadDir(filepath.Join(a.Home, "skills"))
	for _, entry := range entries {
		if entry.IsDir() {
			m.agentFiles = append(m.agentFiles, agentFile{"Skills / " + entry.Name(), filepath.Join(a.Home, "skills", entry.Name(), "SKILL.md")})
		}
	}
	out, err := exec.Command("git", "-C", a.Worktree, "ls-files", "-z").Output()
	if err == nil {
		for _, name := range strings.Split(string(out), "\x00") {
			if name != "" && len(m.agentFiles) < 200 {
				m.agentFiles = append(m.agentFiles, agentFile{"Worktree / " + name, filepath.Join(a.Worktree, name)})
			}
		}
	}
	m.showAgentFiles = true
}

func (m Model) updateAgentFiles(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		if m.agentFileContent != "" {
			m.agentFileContent = ""
		} else {
			m.showAgentFiles = false
		}
	case "up":
		if m.agentFileContent != "" {
			m.agentFileScroll = max(0, m.agentFileScroll-1)
		} else {
			m.agentFileIndex = max(0, m.agentFileIndex-1)
		}
	case "down":
		if m.agentFileContent != "" {
			m.agentFileScroll = min(m.agentFileScroll+1, max(0, len(strings.Split(m.agentFileContent, "\n"))-1))
		} else {
			m.agentFileIndex = min(m.agentFileIndex+1, max(0, len(m.agentFiles)-1))
		}
	case "enter":
		if m.agentFileIndex < len(m.agentFiles) {
			f, err := os.Open(m.agentFiles[m.agentFileIndex].path)
			if err != nil {
				m.setToast(err.Error())
			} else {
				data, err := io.ReadAll(io.LimitReader(f, 65536))
				f.Close()
				if err != nil {
					m.setToast(err.Error())
				} else {
					m.agentFileContent = string(data)
					m.agentFileScroll = 0
				}
			}
		}
	}
	return m, nil
}

func (m Model) agentFilesView(width, height int) string {
	lines := []string{"Agent files", "Enter: preview • ↑/↓: navigate/scroll • Esc: back"}
	if m.agentFileContent != "" {
		content := strings.Split(m.agentFileContent, "\n")
		start := min(m.agentFileScroll, len(content))
		lines = append(lines, content[start:]...)
	} else {
		start := max(0, m.agentFileIndex-max(1, height-5)+1)
		for i := start; i < len(m.agentFiles) && i < start+max(1, height-5); i++ {
			prefix := "  "
			if i == m.agentFileIndex {
				prefix = "> "
			}
			lines = append(lines, prefix+m.agentFiles[i].label)
		}
	}
	return boundedPanel(lines, width, height)
}

func (m *Model) cycleAgentSession(delta int) {
	a := m.selectedAgent()
	if a == nil {
		return
	}
	var indices []int
	current := 0
	for i, item := range m.treeItems {
		if item.Session == nil {
			continue
		}
		cfg, _ := deck.ReadSessionConfig(item.Session)
		if cfg.AgentID == a.ID {
			if i == m.selectedTreeIdx {
				current = len(indices)
			}
			indices = append(indices, i)
		}
	}
	if len(indices) > 0 {
		m.selectedTreeIdx = indices[(current+delta+len(indices))%len(indices)]
		_ = m.updateHoveredInspection()
	}
}

func (m Model) startAgentSession(tool string) (tea.Model, tea.Cmd) {
	a := m.selectedAgent()
	if a == nil || m.sdb == nil {
		m.setToast("Select an agent first.")
		return m, nil
	}
	inst, err := m.sdb.CreateTask(m.v, deck.CreateTaskOptions{AgentID: a.ID, Tool: tool})
	if err != nil {
		m.setToast(err.Error())
		return m, nil
	}
	m.reload()
	for i, item := range m.treeItems {
		if item.Session != nil && item.Session.ID == inst.ID {
			m.selectedTreeIdx = i
			break
		}
	}
	m.setToast(fmt.Sprintf("Started %s session for %s", inst.Tool, a.Name))
	return m, nil
}

func (m Model) agentWorkspaceBar(width int) string {
	a := m.selectedAgent()
	if a == nil {
		return ""
	}
	tabs := []string{a.Name}
	for _, item := range m.treeItems {
		if item.Session == nil {
			continue
		}
		cfg, _ := deck.ReadSessionConfig(item.Session)
		if cfg.AgentID == a.ID {
			label := item.Session.Title
			if inst := m.selectedInstance(); inst != nil && inst.ID == item.Session.ID {
				label = "[" + label + "]"
			}
			tabs = append(tabs, label)
		}
	}
	return truncateString(strings.Join(tabs, " | "), width) + "\n" + truncateString("Alt+←/→: tabs • Alt+1: Codex • Alt+2: Claude • Alt+3: agy • m: files", width)
}

func fitViewport(content string, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i := range lines {
		lines[i] = ansi.Truncate(lines[i], width, "")
	}
	return strings.Join(lines, "\n")
}
