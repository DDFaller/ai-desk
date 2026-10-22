package ui

import "github.com/charmbracelet/lipgloss"

var (
	primaryColor   = lipgloss.Color("#7aa2f7") // Vibrant Blue
	secondaryColor = lipgloss.Color("#bb9af7") // Purple
	accentColor    = lipgloss.Color("#e0af68") // Amber/Orange
	successColor   = lipgloss.Color("#9ece6a") // Green
	warningColor   = lipgloss.Color("#ff9e64") // Warm Orange
	errorColor     = lipgloss.Color("#f7768e") // Coral Red
	subtleColor    = lipgloss.Color("#565f89") // Muted Slate
	bgDark         = lipgloss.Color("#1a1b26") // Deep Background

	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(primaryColor).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#24283b")).
			Padding(0, 1)

	badgeStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(primaryColor).
			Padding(0, 1)

	activeBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(primaryColor).
			Padding(0, 1)

	inactiveBorder = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(subtleColor).
			Padding(0, 1)

	selectedRow = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#ffffff")).
			Background(lipgloss.Color("#3b4261")).
			Padding(0, 1)

	normalRow = lipgloss.NewStyle().
			Padding(0, 1)

	meterFillGreen = lipgloss.NewStyle().
			Foreground(successColor)

	meterFillYellow = lipgloss.NewStyle().
			Foreground(warningColor)

	meterFillRed = lipgloss.NewStyle().
			Foreground(errorColor)

	meterEmpty = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#414868"))

	helpKeyStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(secondaryColor)

	helpDescStyle = lipgloss.NewStyle().
			Foreground(subtleColor)

	statusToastStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("#1a1b26")).
				Background(accentColor).
				Bold(true).
				Padding(0, 1)
)
