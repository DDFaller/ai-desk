package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/falluba/ai-desk/pkg/deck"
)

// RenderInspectorView renders the full-screen diagnostics view for a session.
func RenderInspectorView(insp *deck.SessionInspection, width, height int) string {
	bStyle := activeBorder

	var lines []string

	// Header line
	statusColor := successColor
	if insp.Instance.Status == "error" {
		statusColor = errorColor
	} else if insp.Instance.Status == "waiting" {
		statusColor = warningColor
	}
	statusBadge := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(strings.ToUpper(insp.Instance.Status))
	if insp.Attached {
		statusBadge += " " + lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("[ATTACHED]")
	}

	header := fmt.Sprintf("SESSION INSPECTOR: %s [%s]  %s",
		lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Render(insp.Instance.Title),
		lipgloss.NewStyle().Foreground(subtleColor).Render(insp.Instance.ID),
		statusBadge,
	)
	lines = append(lines, headerStyle.Render(header))
	lines = append(lines, "")

	// Section 1: Overview
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Metadata & Runtime:"))
	lines = append(lines, fmt.Sprintf("  • Group:       %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#38bdf8")).Render(insp.Instance.GroupPath)))
	lines = append(lines, fmt.Sprintf("  • Directory:   %s", lipgloss.NewStyle().Foreground(subtleColor).Render(insp.Instance.ProjectPath)))
	lines = append(lines, fmt.Sprintf("  • Tool:        %s", insp.Instance.Tool))
	if insp.Instance.TmuxSession != "" {
		lines = append(lines, fmt.Sprintf("  • Tmux:        %s", lipgloss.NewStyle().Foreground(subtleColor).Render(insp.Instance.TmuxSession)))
	}
	if insp.Uptime > 0 {
		lines = append(lines, fmt.Sprintf("  • Uptime:      %s", insp.Uptime.Round(time.Second)))
	}
	lines = append(lines, "")

	// Section 2: Active Skills & Prompt Tax
	taxColor := successColor
	if insp.PromptTaxTurn > 2000 {
		taxColor = errorColor
	} else if insp.PromptTaxTurn > 800 {
		taxColor = warningColor
	}
	taxBadge := lipgloss.NewStyle().Foreground(taxColor).Bold(true).Render(fmt.Sprintf("~%d tokens/turn", insp.PromptTaxTurn))

	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Active Skills & Prompt Overhead:"))
	lines = append(lines, fmt.Sprintf("  • Scoped Profile:  %s (%d skills)  %s",
		lipgloss.NewStyle().Bold(true).Render(insp.ActiveProfile),
		len(insp.ActiveSkills),
		taxBadge,
	))
	if len(insp.ActiveSkills) > 0 {
		skillsStr := strings.Join(insp.ActiveSkills, ", ")
		if len(skillsStr) > width-25 {
			skillsStr = skillsStr[:width-28] + "..."
		}
		lines = append(lines, fmt.Sprintf("  • Active Skills:   %s", lipgloss.NewStyle().Foreground(subtleColor).Render(skillsStr)))
	}
	lines = append(lines, "")

	// Section 3: Token Usage & RTK
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("Token Telemetry & Optimization:"))
	if insp.HasTokenMetrics {
		lines = append(lines, fmt.Sprintf("  • Total Tokens:    %s (Input: %s, Output: %s)",
			lipgloss.NewStyle().Bold(true).Render(formatNumber(insp.TokensUsed.TotalTokens)),
			formatNumber(insp.TokensUsed.InputTokens),
			formatNumber(insp.TokensUsed.OutputTokens),
		))
		if insp.TokensUsed.CachedInputTokens > 0 {
			cachePct := float64(insp.TokensUsed.CachedInputTokens) / float64(insp.TokensUsed.InputTokens) * 100.0
			lines = append(lines, fmt.Sprintf("  • Cached Input:    %s (%s cache hit rate)",
				formatNumber(insp.TokensUsed.CachedInputTokens),
				lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(fmt.Sprintf("%.1f%%", cachePct)),
			))
		}
		if insp.TokensUsed.ReasoningTokens > 0 {
			lines = append(lines, fmt.Sprintf("  • Reasoning Out:   %s tokens", formatNumber(insp.TokensUsed.ReasoningTokens)))
		}
	} else {
		lines = append(lines, "  • Turn Tokens:     No token metrics recorded in transcript yet.")
	}

	if insp.RTKSavings != nil && insp.RTKSavings.TotalCommands > 0 {
		rtkBadge := lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(fmt.Sprintf("saved %.1f%% of terminal output", insp.RTKSavings.AvgSavingsPct*100.0))
		lines = append(lines, fmt.Sprintf("  • RTK Savings:     %s tokens saved across %d commands (%s)",
			formatNumber(int64(insp.RTKSavings.TotalSaved)),
			insp.RTKSavings.TotalCommands,
			rtkBadge,
		))
	}
	lines = append(lines, "")

	// Section 4: Live Terminal Preview
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#c084fc")).Render("Live Terminal Capture (Last 8 lines):"))
	previewCount := len(insp.TerminalPreview)
	startIdx := 0
	if previewCount > 8 {
		startIdx = previewCount - 8
	}
	for i := startIdx; i < previewCount; i++ {
		l := insp.TerminalPreview[i]
		if len(l) > width-8 {
			l = l[:width-11] + "..."
		}
		lines = append(lines, "  "+lipgloss.NewStyle().Foreground(subtleColor).Render("│ ")+l)
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Keyboard: Esc / i: Close Inspector • Enter: Attach • s: Manage Skills • d: Delete Session"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func formatNumber(n int64) string {
	if n < 1000 {
		return fmt.Sprintf("%d", n)
	}
	if n < 1000000 {
		return fmt.Sprintf("%.1fK", float64(n)/1000.0)
	}
	return fmt.Sprintf("%.2fM", float64(n)/1000000.0)
}
