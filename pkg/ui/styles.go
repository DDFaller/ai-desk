package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

var (
	primaryColor   = lipgloss.Color("#7aa2f7") // Vibrant Electric Blue
	secondaryColor = lipgloss.Color("#bb9af7") // Lavender / Purple
	accentColor    = lipgloss.Color("#e0af68") // Warm Amber / Gold
	successColor   = lipgloss.Color("#9ece6a") // Vibrant Emerald Green
	warningColor   = lipgloss.Color("#ff9e64") // Warm Orange
	errorColor     = lipgloss.Color("#f7768e") // Coral Red
	cyanColor      = lipgloss.Color("#7dcfff") // Neon Cyan
	subtleColor    = lipgloss.Color("#565f89") // Muted Slate
	bgDark         = lipgloss.Color("#1a1b26") // Deep Midnight
	bgCard         = lipgloss.Color("#24283b") // Elevated Card Background
	bgHighlight    = lipgloss.Color("#2f3549") // Selected Item Background

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(lipgloss.Color("#7aa2f7")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#24283b")).
			Padding(0, 1)

	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(secondaryColor).
			Padding(0, 1)

	activeBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	inactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#3b4261")).
			Padding(0, 1)

	selectedRow = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(bgHighlight).
			Padding(0, 1)

	normalRow = lipgloss.NewStyle().
			Padding(0, 1)

	cardHeader = lipgloss.NewStyle().
			Bold(true).
			Foreground(accentColor)

	helpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(secondaryColor).
			Padding(0, 1)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#a9b1d6"))

	statusToastStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1b26")).
				Background(accentColor).
				Bold(true).
				Padding(0, 1)
)

// RenderToolBadge renders a distinct, colored badge for an AI CLI harness.
func RenderToolBadge(tool string) string {
	lower := strings.ToLower(strings.TrimSpace(tool))
	switch lower {
	case "agy", "gemini", "antigravity":
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(successColor).
			Padding(0, 1).
			Render("agy")
	case "claude", "claude-code":
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(primaryColor).
			Padding(0, 1).
			Render("claude")
	case "codex", "codex-cli":
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(warningColor).
			Padding(0, 1).
			Render("codex")
	case "opencode":
		return lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(secondaryColor).
			Padding(0, 1).
			Render("opencode")
	case "shell", "bash":
		return lipgloss.NewStyle().
			Foreground(subtleColor).
			Render("[shell]")
	default:
		if tool == "" {
			return ""
		}
		return lipgloss.NewStyle().
			Foreground(subtleColor).
			Render("[" + lower + "]")
	}
}
