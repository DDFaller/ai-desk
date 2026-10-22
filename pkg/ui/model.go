package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/rtk"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

type FocusPane int

const (
	FocusSelection FocusPane = iota
	FocusTask
	FocusStatus
)

type Model struct {
	focus            FocusPane
	sessions         []SessionItem
	selectedSession  int
	accounts         []rotation.AccountStatus
	selectedAccount  int
	providerList     []providers.ProviderConfig
	selectedProvider int
	activeAccount    string

	// Hierarchical Groups & Tree (Pane 1: Selection)
	sdb             *deck.StateDB
	treeRoots       []*deck.GroupNode
	treeItems       []deck.TreeItem
	selectedTreeIdx int

	// The Task (Pane 2: Live Terminal Capture)
	taskScrollOffset int

	// Per-Session Status (Pane 3: Live Telemetry)
	currentInspection *deck.SessionInspection

	// Full-screen Modals
	showInspector     bool
	showSkillsOverlay bool
	skillsConfig      *harness.SkillProfilesConfig
	selectedProfile   int
	profileKeys       []string

	// Cascade Delete Dialog
	showDeleteDialog bool
	deletePrompt     string
	deleteIsGroup    bool
	deleteTarget     string

	width  int
	height int

	toast       string
	toastExpiry time.Time
	err         error

	sb  *swapbridge.SwapBridge
	v   *harness.Virtualizer
	reg *providers.Registry
}

func NewModel() Model {
	sb := swapbridge.NewSwapBridge("")
	v := harness.NewVirtualizer("", "")
	reg, _ := providers.Load("")
	skillsCfg, _ := harness.LoadSkillProfiles("")

	providerSlice := make([]providers.ProviderConfig, 0, len(reg.Providers))
	for _, p := range reg.Providers {
		providerSlice = append(providerSlice, p)
	}

	pKeys := make([]string, 0, len(skillsCfg.Profiles))
	for k := range skillsCfg.Profiles {
		pKeys = append(pKeys, k)
	}

	m := Model{
		focus:        FocusSelection,
		sb:           sb,
		v:            v,
		reg:          reg,
		skillsConfig: skillsCfg,
		providerList: providerSlice,
		profileKeys:  pKeys,
	}
	m.reload()
	if len(m.accounts) > 0 {
		m.activeAccount = m.accounts[0].Email
	}
	return m
}

func (m *Model) reload() {
	m.sessions = FetchTmuxSessions()
	accounts, _ := m.sb.LoadAccounts()
	m.accounts = accounts

	// Load hierarchical tree from state.db
	if sdb, err := deck.OpenStateDB(""); err == nil {
		m.sdb = sdb
		groups, _ := sdb.LoadGroups()
		instances, _ := sdb.LoadInstances()
		m.treeRoots = deck.BuildGroupTree(groups, instances)
		m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
	}

	if m.selectedTreeIdx >= len(m.treeItems) && len(m.treeItems) > 0 {
		m.selectedTreeIdx = len(m.treeItems) - 1
	}
	if m.selectedSession >= len(m.sessions) && len(m.sessions) > 0 {
		m.selectedSession = len(m.sessions) - 1
	}
	if m.selectedAccount >= len(m.accounts) && len(m.accounts) > 0 {
		m.selectedAccount = len(m.accounts) - 1
	}

	m.updateHoveredInspection()
}

func (m *Model) updateHoveredInspection() {
	if len(m.treeItems) == 0 || m.selectedTreeIdx < 0 || m.selectedTreeIdx >= len(m.treeItems) {
		m.currentInspection = nil
		return
	}
	item := m.treeItems[m.selectedTreeIdx]
	if !item.IsGroup && item.Session != nil {
		if m.sdb != nil {
			insp, err := m.sdb.InspectSession(item.Session.ID)
			if err == nil {
				m.currentInspection = insp
				return
			}
		}
	} else if item.IsGroup && item.GroupNode != nil {
		m.currentInspection = &deck.SessionInspection{
			Instance: deck.InstanceRecord{
				Title:       item.GroupNode.Name,
				GroupPath:   item.GroupNode.Path,
				Status:      "group",
				Tool:        "group",
			},
			ActiveProfile: "group",
		}
	}
}

func (m Model) Init() tea.Cmd {
	return nil
}

type attachFinishedMsg struct{ err error }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case attachFinishedMsg:
		m.reload()
		m.setToast("Detached from session.")
		return m, nil

	case tea.KeyMsg:
		key := msg.String()

		// 1. Delete Confirmation Dialog Modal
		if m.showDeleteDialog {
			switch key {
			case "y", "Y":
				m.showDeleteDialog = false
				if m.deleteIsGroup {
					if m.sdb != nil {
						deleted, err := m.sdb.DeleteGroupCascade(m.deleteTarget)
						if err != nil {
							m.setToast(fmt.Sprintf("Cascade delete failed: %v", err))
						} else {
							m.setToast(fmt.Sprintf("✓ Group %q and all %d tasks deleted.", m.deleteTarget, len(deleted)))
							m.reload()
						}
					}
				} else {
					_ = exec.Command("agent-deck", "session", "remove", "--force", m.deleteTarget).Run()
					m.setToast("✓ Session removed.")
					m.reload()
				}
				return m, nil

			case "n", "N", "esc", "q":
				m.showDeleteDialog = false
				m.setToast("Deletion cancelled.")
				return m, nil
			}
			return m, nil
		}

		// 2. Full-Screen Session Inspector Modal
		if m.showInspector {
			switch key {
			case "esc", "i", "q":
				m.showInspector = false
				return m, nil
			case "enter", "a":
				if m.currentInspection != nil && m.currentInspection.Instance.TmuxSession != "" {
					m.showInspector = false
					c := exec.Command("tmux", "attach-session", "-t", m.currentInspection.Instance.TmuxSession)
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						return attachFinishedMsg{err: err}
					})
				}
			}
			return m, nil
		}

		// 3. Global Navigation Keys
		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "q":
			if m.showSkillsOverlay {
				m.showSkillsOverlay = false
				return m, nil
			}
			return m, tea.Quit

		case "s":
			m.showSkillsOverlay = !m.showSkillsOverlay
			if m.showSkillsOverlay {
				m.setToast("Skills Manager opened.")
			} else {
				m.setToast("Skills Manager closed.")
			}
			return m, nil

		case "i":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if !item.IsGroup && item.Session != nil {
					if m.sdb != nil {
						insp, err := m.sdb.InspectSession(item.Session.ID)
						if err == nil {
							m.currentInspection = insp
							m.showInspector = true
							return m, nil
						}
					}
				} else if item.IsGroup && item.GroupNode != nil {
					m.setToast(fmt.Sprintf("Group %q (%d tasks). Space to expand/collapse.", item.GroupNode.Path, item.GroupNode.TotalTasks))
				}
			}
			return m, nil

		case "d":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					m.showDeleteDialog = true
					m.deleteIsGroup = true
					m.deleteTarget = item.GroupNode.Path
					m.deletePrompt = fmt.Sprintf("Cascade delete group %q and ALL %d sessions inside it?", item.GroupNode.Path, item.GroupNode.TotalTasks)
				} else if !item.IsGroup && item.Session != nil {
					m.showDeleteDialog = true
					m.deleteIsGroup = false
					m.deleteTarget = item.Session.ID
					m.deletePrompt = fmt.Sprintf("Delete session %q [%s]?", item.Session.Title, item.Session.ID)
				}
			}
			return m, nil

		case "tab":
			if !m.showSkillsOverlay {
				m.focus = (m.focus + 1) % 3
			}
			return m, nil

		case "h", "left":
			if !m.showSkillsOverlay {
				m.focus = (m.focus + 2) % 3
			}
			return m, nil

		case "l", "right":
			if !m.showSkillsOverlay {
				m.focus = (m.focus + 1) % 3
			}
			return m, nil

		case "up", "k":
			if m.showSkillsOverlay {
				if m.selectedProfile > 0 {
					m.selectedProfile--
				}
			} else if m.focus == FocusSelection {
				if len(m.treeItems) > 0 && m.selectedTreeIdx > 0 {
					m.selectedTreeIdx--
					m.taskScrollOffset = 0
					m.updateHoveredInspection()
				}
			} else if m.focus == FocusTask {
				m.taskScrollOffset++
			} else if m.focus == FocusStatus {
				if m.selectedAccount > 0 {
					m.selectedAccount--
				}
			}
			return m, nil

		case "down", "j":
			if m.showSkillsOverlay {
				if m.selectedProfile < len(m.profileKeys)-1 {
					m.selectedProfile++
				}
			} else if m.focus == FocusSelection {
				if len(m.treeItems) > 0 && m.selectedTreeIdx < len(m.treeItems)-1 {
					m.selectedTreeIdx++
					m.taskScrollOffset = 0
					m.updateHoveredInspection()
				}
			} else if m.focus == FocusTask {
				if m.taskScrollOffset > 0 {
					m.taskScrollOffset--
				}
			} else if m.focus == FocusStatus {
				if m.selectedAccount < len(m.accounts)-1 {
					m.selectedAccount++
				}
			}
			return m, nil

		case "pgup":
			if m.focus == FocusTask {
				m.taskScrollOffset += 8
			}
			return m, nil

		case "pgdown":
			if m.focus == FocusTask {
				if m.taskScrollOffset > 8 {
					m.taskScrollOffset -= 8
				} else {
					m.taskScrollOffset = 0
				}
			}
			return m, nil

		case " ":
			// Toggle expand/collapse on group in Selection pane
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					item.GroupNode.Expanded = !item.GroupNode.Expanded
					m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
					if m.selectedTreeIdx >= len(m.treeItems) {
						m.selectedTreeIdx = len(m.treeItems) - 1
					}
					m.updateHoveredInspection()
				}
			}
			return m, nil

		case "r":
			m.reload()
			m.setToast("State, tree & quota refreshed.")
			return m, nil

		case "enter":
			if m.showSkillsOverlay {
				if len(m.profileKeys) > 0 {
					pName := m.profileKeys[m.selectedProfile]
					m.setToast(fmt.Sprintf("Profile %q selected for new sessions.", pName))
				}
				return m, nil
			}

			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					// Toggle expand/collapse
					item.GroupNode.Expanded = !item.GroupNode.Expanded
					m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
					if m.selectedTreeIdx >= len(m.treeItems) {
						m.selectedTreeIdx = len(m.treeItems) - 1
					}
					m.updateHoveredInspection()
					return m, nil
				} else if !item.IsGroup && item.Session != nil {
					sessName := item.Session.TmuxSession
					if sessName == "" {
						sessName = item.Session.ID
					}
					c := exec.Command("tmux", "attach-session", "-t", sessName)
					return m, tea.ExecProcess(c, func(err error) tea.Msg {
						return attachFinishedMsg{err: err}
					})
				}
			} else if len(m.sessions) > 0 {
				sess := m.sessions[m.selectedSession]
				c := exec.Command("tmux", "attach-session", "-t", sess.ID)
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					return attachFinishedMsg{err: err}
				})
			}
		}
	}

	return m, nil
}

func sbReadToken(sb *swapbridge.SwapBridge, email string) (string, error) {
	return sb.ReadAccountToken(email)
}

func (m *Model) setToast(msg string) {
	m.toast = msg
	m.toastExpiry = time.Now().Add(4 * time.Second)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading AI Desk..."
	}

	// 1. Top Header
	header := m.renderHeader()

	// 2. Main Content
	contentHeight := m.height - 7
	if contentHeight < 10 {
		contentHeight = 10
	}

	var mainContent string
	if m.showDeleteDialog {
		mainContent = m.renderDeleteDialog(m.width-4, contentHeight)
	} else if m.showInspector && m.currentInspection != nil {
		mainContent = RenderInspectorView(m.currentInspection, m.width-4, contentHeight)
	} else if m.showSkillsOverlay {
		mainContent = m.renderSkillsOverlay(m.width-4, contentHeight)
	} else {
		// 3-Pane Side-by-Side Layout
		availWidth := m.width - 6
		if availWidth < 80 {
			availWidth = 80
		}

		// Pane 1 (Selection): ~28%
		col1Width := (availWidth * 28) / 100
		if col1Width < 26 {
			col1Width = 26
		}

		// Pane 3 (Session Status): ~30%
		col3Width := (availWidth * 30) / 100
		if col3Width < 28 {
			col3Width = 28
		}

		// Pane 2 (The Task): remaining ~42%
		col2Width := availWidth - col1Width - col3Width
		if col2Width < 30 {
			col2Width = 30
		}

		pane1 := m.renderSelectionPane(col1Width, contentHeight)
		pane2 := m.renderTaskPane(col2Width, contentHeight)
		pane3 := m.renderStatusPane(col3Width, contentHeight)

		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, pane1, " ", pane2, " ", pane3)
	}

	// 3. Footer / Shortcuts
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, mainContent, footer)
}

func (m Model) renderHeader() string {
	rtkBadge := lipgloss.NewStyle().Foreground(subtleColor).Render("[RTK: -]")
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		rtkBadge = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[RTK: Active]")
	}
	title := titleStyle.Render("AI DESK") + badgeStyle.Render("v0.2.0") + " " + rtkBadge
	activeInfo := ""
	if m.activeAccount != "" {
		activeInfo = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("Active: " + m.activeAccount)
	}

	toast := ""
	if time.Now().Before(m.toastExpiry) && m.toast != "" {
		toast = statusToastStyle.Render(m.toast)
	}

	gapWidth := m.width - lipgloss.Width(title) - lipgloss.Width(activeInfo) - lipgloss.Width(toast) - 4
	if gapWidth < 1 {
		gapWidth = 1
	}
	spacer := strings.Repeat(" ", gapWidth)

	topLine := lipgloss.JoinHorizontal(lipgloss.Center, title, spacer, toast, " ", activeInfo)
	return lipgloss.NewStyle().MarginBottom(1).Render(topLine)
}

// Pane 1: Selection (Hierarchical Tree of Groups & Tasks)
func (m Model) renderSelectionPane(width, height int) string {
	bStyle := inactiveBorder
	if m.focus == FocusSelection {
		bStyle = activeBorder
	}

	title := fmt.Sprintf("1. Selection (%d items)", len(m.treeItems))
	var lines []string

	if len(m.treeItems) == 0 && len(m.sessions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No active groups or sessions."))
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Spawn one with: ai-desk run <tool>"))
	} else if len(m.treeItems) > 0 {
		for i, item := range m.treeItems {
			indent := strings.Repeat("  ", item.Depth)
			isSelected := i == m.selectedTreeIdx

			if item.IsGroup && item.GroupNode != nil {
				prefix := "▼ "
				if !item.GroupNode.Expanded {
					prefix = "▶ "
				}
				rowText := fmt.Sprintf("%s%s%s (%d)", indent, prefix, item.GroupNode.Name, item.GroupNode.TotalTasks)
				styledRow := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#f59e0b")).Render(rowText)
				if isSelected {
					lines = append(lines, selectedRow.Width(width-4).Render("▶ "+rowText))
				} else {
					lines = append(lines, normalRow.Render("  "+styledRow))
				}
			} else if !item.IsGroup && item.Session != nil {
				statusBullet := "●"
				bulletColor := subtleColor
				switch item.Session.Status {
				case "running":
					bulletColor = successColor
				case "waiting":
					bulletColor = warningColor
				case "error":
					bulletColor = errorColor
				}

				bullet := lipgloss.NewStyle().Foreground(bulletColor).Render(statusBullet)
				toolBadge := lipgloss.NewStyle().Foreground(subtleColor).Render("[" + item.Session.Tool + "]")
				titleStr := truncateString(item.Session.Title, width-item.Depth*2-22)
				rowContent := fmt.Sprintf("%s%s %-14s %s", indent, bullet, titleStr, toolBadge)

				if isSelected {
					lines = append(lines, selectedRow.Width(width-4).Render("▶ "+rowContent))
				} else {
					lines = append(lines, normalRow.Render("  "+rowContent))
				}
			}
		}
	} else {
		for i, s := range m.sessions {
			badge := lipgloss.NewStyle().Foreground(subtleColor).Render("[IDLE]")
			if s.Attached {
				badge = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[ATTACHED]")
			}
			titleStr := truncateString(s.Title, width-20)
			rowContent := fmt.Sprintf("%-16s %s", titleStr, badge)
			if i == m.selectedSession {
				lines = append(lines, selectedRow.Width(width-4).Render("▶ "+rowContent))
			} else {
				lines = append(lines, normalRow.Render("  "+rowContent))
			}
		}
	}

	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

// Pane 2: The Task (Live Terminal Capture Preview)
func (m Model) renderTaskPane(width, height int) string {
	bStyle := inactiveBorder
	if m.focus == FocusTask {
		bStyle = activeBorder
	}

	var title string
	var lines []string

	if len(m.treeItems) == 0 {
		title = "2. Task (No Active Session)"
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No session selected."))
	} else {
		item := m.treeItems[m.selectedTreeIdx]

		if item.IsGroup && item.GroupNode != nil {
			title = fmt.Sprintf("2. Task: Group %s", item.GroupNode.Name)
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Group Overview:"))
			lines = append(lines, fmt.Sprintf("  • Full Path:       %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#38bdf8")).Render(item.GroupNode.Path)))
			lines = append(lines, fmt.Sprintf("  • Total Sessions:  %d tasks", item.GroupNode.TotalTasks))
			lines = append(lines, fmt.Sprintf("  • Subgroups:       %d nested", len(item.GroupNode.Children)))
			lines = append(lines, fmt.Sprintf("  • Direct Tasks:    %d", len(item.GroupNode.Sessions)))
			lines = append(lines, "")
			lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Shortcuts for Group:"))
			lines = append(lines, "  • [Space] or [Enter] : Toggle expand/collapse")
			lines = append(lines, "  • [d]                 : Cascade delete group and ALL sessions")
			lines = append(lines, "  • [g]                 : Create new subgroup")
		} else if !item.IsGroup && item.Session != nil {
			title = fmt.Sprintf("2. Task: %s [%s]", item.Session.Title, strings.ToUpper(item.Session.Status))

			if m.currentInspection != nil && len(m.currentInspection.TerminalPreview) > 0 {
				preview := m.currentInspection.TerminalPreview
				availLines := height - 4
				if availLines < 4 {
					availLines = 4
				}

				totalLines := len(preview)
				endIdx := totalLines - m.taskScrollOffset
				if endIdx > totalLines {
					endIdx = totalLines
				}
				if endIdx < availLines {
					endIdx = availLines
				}
				startIdx := endIdx - availLines
				if startIdx < 0 {
					startIdx = 0
				}

				if m.taskScrollOffset > 0 {
					lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render(fmt.Sprintf("[↑ History Scroll -%d lines]", m.taskScrollOffset)))
				}

				for i := startIdx; i < endIdx && i < totalLines; i++ {
					l := preview[i]
					if len(l) > width-6 {
						l = l[:width-9] + "..."
					}
					lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("│ ")+l)
				}
			} else {
				lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Terminal preview unavailable (session idle or detached)."))
				lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Press [Enter] to attach interactive terminal."))
			}
		}
	}

	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

// Pane 3: Per-Session Status (Live Telemetry & Diagnostics)
func (m Model) renderStatusPane(width, height int) string {
	bStyle := inactiveBorder
	if m.focus == FocusStatus {
		bStyle = activeBorder
	}

	title := "3. Session Status"
	var lines []string

	if m.currentInspection != nil && m.currentInspection.Instance.Status != "group" {
		insp := m.currentInspection

		// 1. Session Identity
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Session Info:"))
		lines = append(lines, fmt.Sprintf("  • Title:   %s", lipgloss.NewStyle().Bold(true).Render(truncateString(insp.Instance.Title, width-16))))
		lines = append(lines, fmt.Sprintf("  • Tool:    %s", insp.Instance.Tool))
		if insp.Instance.TmuxSession != "" {
			lines = append(lines, fmt.Sprintf("  • Tmux:    %s", truncateString(insp.Instance.TmuxSession, width-16)))
		}
		if insp.Uptime > 0 {
			lines = append(lines, fmt.Sprintf("  • Uptime:  %s", insp.Uptime.Round(time.Second)))
		}
		lines = append(lines, "")

		// 2. Active Skill Profile & Prompt Tax
		taxColor := successColor
		if insp.PromptTaxTurn > 2000 {
			taxColor = errorColor
		} else if insp.PromptTaxTurn > 800 {
			taxColor = warningColor
		}
		taxBadge := lipgloss.NewStyle().Foreground(taxColor).Bold(true).Render(fmt.Sprintf("~%d tok/turn", insp.PromptTaxTurn))

		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Active Skills & Tax:"))
		lines = append(lines, fmt.Sprintf("  • Profile: %s  %s", lipgloss.NewStyle().Bold(true).Render(insp.ActiveProfile), taxBadge))
		if len(insp.ActiveSkills) > 0 {
			skillsSummary := strings.Join(insp.ActiveSkills, ", ")
			if len(skillsSummary) > width-16 {
				skillsSummary = skillsSummary[:width-19] + "..."
			}
			lines = append(lines, fmt.Sprintf("  • Skills:  %s", lipgloss.NewStyle().Foreground(subtleColor).Render(skillsSummary)))
		}
		lines = append(lines, "")

		// 3. Token Telemetry
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Token Usage:"))
		if insp.HasTokenMetrics {
			lines = append(lines, fmt.Sprintf("  • Total:   %s", formatNumber(insp.TokensUsed.TotalTokens)))
			lines = append(lines, fmt.Sprintf("  • In/Out:  %s / %s", formatNumber(insp.TokensUsed.InputTokens), formatNumber(insp.TokensUsed.OutputTokens)))
			if insp.TokensUsed.CachedInputTokens > 0 {
				cachePct := float64(insp.TokensUsed.CachedInputTokens) / float64(insp.TokensUsed.InputTokens) * 100.0
				lines = append(lines, fmt.Sprintf("  • Cached:  %s (%s)", formatNumber(insp.TokensUsed.CachedInputTokens), lipgloss.NewStyle().Foreground(successColor).Render(fmt.Sprintf("%.1f%%", cachePct))))
			}
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  • No turn metrics in log."))
		}

		// 4. RTK Savings
		if insp.RTKSavings != nil && insp.RTKSavings.TotalCommands > 0 {
			lines = append(lines, fmt.Sprintf("  • RTK:     %s saved (%.1f%%)", formatNumber(int64(insp.RTKSavings.TotalSaved)), insp.RTKSavings.AvgSavingsPct*100.0))
		}
		lines = append(lines, "")

		// 5. Account & Quota mini-meter
		if len(m.accounts) > 0 {
			acc := m.accounts[m.selectedAccount]
			bar := m.renderProgressBar(acc.RemainingQuotaPct, 10)
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Active Account:"))
			lines = append(lines, fmt.Sprintf("  • %s %4.0f%%", bar, acc.RemainingQuotaPct))
		}

	} else {
		// Group overview in status pane
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Group Status:"))
		if len(m.treeItems) > 0 {
			item := m.treeItems[m.selectedTreeIdx]
			if item.GroupNode != nil {
				lines = append(lines, fmt.Sprintf("  • Name:   %s", item.GroupNode.Name))
				lines = append(lines, fmt.Sprintf("  • Tasks:  %d total", item.GroupNode.TotalTasks))
				lines = append(lines, fmt.Sprintf("  • Path:   %s", item.GroupNode.Path))
			}
		}
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Select a task session to inspect active skills, tokens used, and live telemetry."))
	}

	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

func (m Model) renderDeleteDialog(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 2)

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("⚠ CONFIRM CASCADE DELETION"))
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(m.deletePrompt))
	lines = append(lines, "")
	if m.deleteIsGroup {
		lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("This will terminate all running tmux processes and delete all tasks in this group and all its subgroups."))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("This will terminate the session's tmux process and remove it from the registry."))
	}
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("[y] Confirm & Delete   •   [n/Esc] Cancel"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderSkillsOverlay(width, height int) string {
	bStyle := activeBorder

	var lines []string
	lines = append(lines, headerStyle.Render("Skills Manager (~/.ai-desk/skill-profiles.yaml)"))
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Select a profile to inspect its prompt tax. Extra skills attach consciously at runtime."))
	lines = append(lines, "")

	for i, name := range m.profileKeys {
		pDef := m.skillsConfig.Profiles[name]
		skills, _ := m.skillsConfig.ResolveSkills(name, "")
		count := len(skills)
		tokenTax := count * 60
		if count >= 70 {
			tokenTax = 4500
		}

		taxColor := successColor
		if tokenTax > 2000 {
			taxColor = errorColor
		} else if tokenTax > 800 {
			taxColor = warningColor
		}

		taxBadge := lipgloss.NewStyle().Foreground(taxColor).Bold(true).Render(fmt.Sprintf("~%d tok/turn", tokenTax))
		row := fmt.Sprintf("%-12s  %-14s  (%2d skills)  %s", name, taxBadge, count, pDef.Description)

		if i == m.selectedProfile {
			lines = append(lines, selectedRow.Width(width-4).Render("▶ "+row))
		} else {
			lines = append(lines, normalRow.Render("  "+row))
		}
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(accentColor).Render("CLI Quick Command: ai-desk skills attach <session> <skill_name>"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderProgressBar(pct float64, width int) string {
	filled := int((pct / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	color := successColor
	if pct < 20.0 {
		color = errorColor
	} else if pct < 50.0 {
		color = warningColor
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}

func (m Model) renderFooter() string {
	var keys []string

	if m.showInspector {
		keys = []string{
			helpKeyStyle.Render("Esc/i") + " " + helpDescStyle.Render("Close Inspector"),
			helpKeyStyle.Render("Enter/a") + " " + helpDescStyle.Render("Attach Tmux"),
			helpKeyStyle.Render("s") + " " + helpDescStyle.Render("Skills"),
			helpKeyStyle.Render("q") + " " + helpDescStyle.Render("Quit"),
		}
	} else if m.showDeleteDialog {
		keys = []string{
			helpKeyStyle.Render("y") + " " + helpDescStyle.Render("Confirm Cascade Delete"),
			helpKeyStyle.Render("n/Esc") + " " + helpDescStyle.Render("Cancel"),
		}
	} else if m.showSkillsOverlay {
		keys = []string{
			helpKeyStyle.Render("s") + " " + helpDescStyle.Render("Close Skills Manager"),
			helpKeyStyle.Render("↑/↓") + " " + helpDescStyle.Render("Navigate Profiles"),
			helpKeyStyle.Render("Enter") + " " + helpDescStyle.Render("Select Default"),
			helpKeyStyle.Render("q") + " " + helpDescStyle.Render("Quit"),
		}
	} else {
		keys = []string{
			helpKeyStyle.Render("Tab/h/l") + " " + helpDescStyle.Render("Switch Pane"),
			helpKeyStyle.Render("↑/↓") + " " + helpDescStyle.Render("Navigate"),
			helpKeyStyle.Render("Space") + " " + helpDescStyle.Render("Expand/Collapse"),
			helpKeyStyle.Render("Enter") + " " + helpDescStyle.Render("Attach Tmux"),
			helpKeyStyle.Render("d") + " " + helpDescStyle.Render("Delete Group/Task"),
			helpKeyStyle.Render("s") + " " + helpDescStyle.Render("Skills"),
			helpKeyStyle.Render("i") + " " + helpDescStyle.Render("Inspect"),
			helpKeyStyle.Render("r") + " " + helpDescStyle.Render("Refresh"),
			helpKeyStyle.Render("q") + " " + helpDescStyle.Render("Quit"),
		}
	}
	return lipgloss.NewStyle().MarginTop(1).Render(strings.Join(keys, "   •   "))
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}
