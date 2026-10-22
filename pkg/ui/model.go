package ui

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/rtk"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

type FocusPane int

const (
	FocusSessions FocusPane = iota
	FocusAccounts
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

	// Skills Manager
	showSkillsOverlay bool
	skillsConfig      *harness.SkillProfilesConfig
	selectedProfile   int
	profileKeys       []string

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
		focus:        FocusSessions,
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

	if m.selectedSession >= len(m.sessions) && len(m.sessions) > 0 {
		m.selectedSession = len(m.sessions) - 1
	}
	if m.selectedAccount >= len(m.accounts) && len(m.accounts) > 0 {
		m.selectedAccount = len(m.accounts) - 1
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
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		case "s":
			m.showSkillsOverlay = !m.showSkillsOverlay
			if m.showSkillsOverlay {
				m.setToast("Skills Manager opened.")
			} else {
				m.setToast("Skills Manager closed.")
			}
			return m, nil

		case "tab", "l", "h", "left", "right":
			if !m.showSkillsOverlay {
				if m.focus == FocusSessions {
					m.focus = FocusAccounts
				} else {
					m.focus = FocusSessions
				}
			}
			return m, nil

		case "up", "k":
			if m.showSkillsOverlay {
				if m.selectedProfile > 0 {
					m.selectedProfile--
				}
			} else if m.focus == FocusSessions {
				if m.selectedSession > 0 {
					m.selectedSession--
				}
			} else {
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
			} else if m.focus == FocusSessions {
				if m.selectedSession < len(m.sessions)-1 {
					m.selectedSession++
				}
			} else {
				if m.selectedAccount < len(m.accounts)-1 {
					m.selectedAccount++
				}
			}
			return m, nil

		case "r":
			m.reload()
			m.setToast("Quota & sessions refreshed.")
			return m, nil

		case "enter":
			if m.showSkillsOverlay {
				if len(m.profileKeys) > 0 {
					pName := m.profileKeys[m.selectedProfile]
					m.setToast(fmt.Sprintf("Profile %q selected for new sessions.", pName))
				}
				return m, nil
			}
			if m.focus == FocusSessions && len(m.sessions) > 0 {
				sess := m.sessions[m.selectedSession]
				c := exec.Command("tmux", "attach-session", "-t", sess.ID)
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					return attachFinishedMsg{err: err}
				})
			} else if m.focus == FocusAccounts && len(m.accounts) > 0 {
				acc := m.accounts[m.selectedAccount]
				m.activeAccount = acc.Email
				token, _ := sbReadToken(m.sb, acc.Email)
				_ = m.v.WriteAgyToken(acc.Email, token, acc.Email)
				m.setToast(fmt.Sprintf("Active session switched to %s", acc.Email))
				return m, nil
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

	// 1. Header
	header := m.renderHeader()

	// 2. Main content
	contentHeight := m.height - 7
	if contentHeight < 10 {
		contentHeight = 10
	}

	var mainContent string
	if m.showSkillsOverlay {
		mainContent = m.renderSkillsOverlay(m.width-4, contentHeight)
	} else {
		leftWidth := (m.width * 55) / 100
		rightWidth := m.width - leftWidth - 4
		if leftWidth < 30 {
			leftWidth = 30
		}
		if rightWidth < 30 {
			rightWidth = 30
		}

		leftPane := m.renderSessionsPane(leftWidth, contentHeight)
		rightPane := m.renderAccountsPane(rightWidth, contentHeight)
		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, leftPane, " ", rightPane)
	}

	// 3. Footer / Help
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, mainContent, footer)
}

func (m Model) renderHeader() string {
	rtkBadge := lipgloss.NewStyle().Foreground(subtleColor).Render("[RTK: -]")
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		rtkBadge = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[RTK: Active]")
	}
	title := titleStyle.Render("AI DESK") + badgeStyle.Render("v0.1.0") + " " + rtkBadge
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

func (m Model) renderSessionsPane(width, height int) string {
	bStyle := inactiveBorder
	if m.focus == FocusSessions {
		bStyle = activeBorder
	}

	title := "Active Tmux Sessions (" + strconvItoa(len(m.sessions)) + ")"
	var lines []string

	if len(m.sessions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No active tmux sessions found."))
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Spawn one with: ai-desk run <tool>"))
	} else {
		for i, s := range m.sessions {
			badge := lipgloss.NewStyle().Foreground(subtleColor).Render("[IDLE]")
			if s.Attached {
				badge = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[ATTACHED]")
			}

			titleStr := truncateString(s.Title, width-22)
			rowContent := fmt.Sprintf("%-24s  %s  %s", titleStr, badge, s.CreatedAt.Format("15:04"))

			if i == m.selectedSession && m.focus == FocusSessions {
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

func (m Model) renderAccountsPane(width, height int) string {
	bStyle := inactiveBorder
	if m.focus == FocusAccounts {
		bStyle = activeBorder
	}

	var sections []string

	// Accounts Section
	accTitle := headerStyle.Render("Google Accounts (agy-swap)")
	sections = append(sections, accTitle)

	if len(m.accounts) == 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(subtleColor).Render("No accounts in ~/.gemini/agy-swap/accounts.json"))
	} else {
		for i, a := range m.accounts {
			bar := m.renderProgressBar(a.RemainingQuotaPct, 12)
			activeTag := ""
			if a.Email == m.activeAccount {
				activeTag = lipgloss.NewStyle().Foreground(accentColor).Render("●")
			}

			rowContent := fmt.Sprintf("%s %-22s %s %4.0f%%", activeTag, truncateString(a.Email, 20), bar, a.RemainingQuotaPct)
			if i == m.selectedAccount && m.focus == FocusAccounts {
				sections = append(sections, selectedRow.Width(width-4).Render("▶ "+rowContent))
			} else {
				sections = append(sections, normalRow.Render("  "+rowContent))
			}
		}
	}

	// Providers Section
	sections = append(sections, "\n"+headerStyle.Render("Custom Providers (~/.ai-desk/providers.yaml)"))
	if len(m.providerList) == 0 {
		sections = append(sections, lipgloss.NewStyle().Foreground(subtleColor).Render("No custom providers configured."))
	} else {
		for _, p := range m.providerList {
			endpoint := truncateString(p.BaseURL, width-18)
			row := fmt.Sprintf("• %-12s %s", p.Name, lipgloss.NewStyle().Foreground(subtleColor).Render(endpoint))
			sections = append(sections, normalRow.Render(row))
		}
	}

	// RTK Token Optimization Section
	sections = append(sections, "\n"+headerStyle.Render("RTK Token Saver (Rust Token Killer)"))
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		ver, _ := rtkClient.Version()
		statusLine := fmt.Sprintf("• Status: %s (%s)", lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("Active"), ver)
		sections = append(sections, normalRow.Render(statusLine))
		if savings, err := rtkClient.GetSavings(); err == nil && savings.TotalCommands > 0 {
			statLine := fmt.Sprintf("• Commands: %d | Saved: %d tokens (%.1f%%)",
				savings.TotalCommands, savings.TotalSaved, savings.AvgSavingsPct*100)
			sections = append(sections, normalRow.Render(statLine))
		}
	} else {
		sections = append(sections, normalRow.Render("• Status: Not installed (make install-rtk)"))
	}

	body := strings.Join(sections, "\n")
	return bStyle.Width(width).Height(height).Render(body)
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
	barStr := strings.Repeat("█", filled)
	emptyStr := strings.Repeat("░", width-filled)

	fillStyled := meterFillGreen.Render(barStr)
	if pct <= 40 && pct > 15 {
		fillStyled = meterFillYellow.Render(barStr)
	} else if pct <= 15 {
		fillStyled = meterFillRed.Render(barStr)
	}

	return fillStyled + meterEmpty.Render(emptyStr)
}

func (m Model) renderFooter() string {
	keys := []string{
		helpKeyStyle.Render("Tab") + " " + helpDescStyle.Render("Switch Pane"),
		helpKeyStyle.Render("↑/↓") + " " + helpDescStyle.Render("Navigate"),
		helpKeyStyle.Render("Enter") + " " + helpDescStyle.Render("Attach/Select"),
		helpKeyStyle.Render("s") + " " + helpDescStyle.Render("Skills Manager"),
		helpKeyStyle.Render("r") + " " + helpDescStyle.Render("Refresh"),
		helpKeyStyle.Render("q") + " " + helpDescStyle.Render("Quit"),
	}
	if m.showSkillsOverlay {
		keys = []string{
			helpKeyStyle.Render("s") + " " + helpDescStyle.Render("Close Skills Manager"),
			helpKeyStyle.Render("↑/↓") + " " + helpDescStyle.Render("Navigate Profiles"),
			helpKeyStyle.Render("Enter") + " " + helpDescStyle.Render("Select Default"),
			helpKeyStyle.Render("q") + " " + helpDescStyle.Render("Quit"),
		}
	}
	return lipgloss.NewStyle().MarginTop(1).Render(strings.Join(keys, "   •   "))
}

func truncateString(s string, max int) string {
	if max <= 3 {
		return s
	}
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}

func strconvItoa(n int) string {
	return fmt.Sprintf("%d", n)
}
