package voice

import (
	"context"
	"crypto/md5"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// HarnessVoiceAdapter abstracts harness-specific prompt injection,
// turn-completion detection, and spoken output sanitization.
type HarnessVoiceAdapter interface {
	Name() string
	InjectPrompt(sessionName, prompt string) error
	WaitForTurn(ctx context.Context, sessionName string, initialBuf []string, timeout time.Duration) (string, error)
	SanitizeSpokenResponse(raw string) string
}

// SelectVoiceAdapter returns the specialized adapter for a given harness tool.
func SelectVoiceAdapter(tool string) HarnessVoiceAdapter {
	switch strings.ToLower(strings.TrimSpace(tool)) {
	case "agy", "gemini":
		return &AgyVoiceAdapter{}
	case "codex":
		return &CodexVoiceAdapter{}
	case "claude":
		return &ClaudeVoiceAdapter{}
	default:
		return &GenericVoiceAdapter{toolName: tool}
	}
}

// Common Regexes for Voice Sanitization
var (
	ansiRegex      = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]|\x1b\([a-zA-Z]|\x1b\][^\x07\x1b]*(\x07|\x1b\\)`)
	codeFenceRegex = regexp.MustCompile("(?s)```[a-zA-Z0-9_-]*\n(.*?)```")
	spinnerChars   = regexp.MustCompile(`[⠋⠙⠹⠸⠼⠴⠦⠧⠇⠏●▶▲▼■│┌┐└┘├┤─○•]`)
	multipleSpaces = regexp.MustCompile(`[ \t]+`)
	urlRegex       = regexp.MustCompile(`https?://[^\s)]+`)
	fileLinkRegex  = regexp.MustCompile(`\[([^\]]+)\]\(file:///[^\)]+\)`)
	plainFileRegex = regexp.MustCompile(`file:///[^\s)]+`)
	agyToolRegex   = regexp.MustCompile(`(?i)(?:○\s*Thought|●\s*Ran|Command output:|Thinking\.\.\.|Running:|Executing:)`)
)

// CapturePaneLines captures the recent lines of a tmux pane.
func CapturePaneLines(sessionName string, lines int) ([]string, error) {
	if lines <= 0 {
		lines = 100
	}
	cmd := exec.Command("tmux", "capture-pane", "-pt", sessionName, "-S", fmt.Sprintf("-%d", lines))
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("tmux capture-pane for %q: %w", sessionName, err)
	}

	rawLines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	cleanLines := make([]string, 0, len(rawLines))
	for _, l := range rawLines {
		cleanLines = append(cleanLines, strings.TrimRight(l, "\r\n"))
	}
	return cleanLines, nil
}

// extractDiffLines calculates newly added lines between before and after snapshots.
func extractDiffLines(before, after []string) string {
	if len(after) <= len(before) {
		return ""
	}
	diffLines := after[len(before):]
	return strings.Join(diffLines, "\n")
}

func hashPaneLines(lines []string) string {
	combined := strings.Join(lines, "\n")
	return fmt.Sprintf("%x", md5.Sum([]byte(combined)))
}

// --- AgyVoiceAdapter ---

type AgyVoiceAdapter struct{}

func (a *AgyVoiceAdapter) Name() string {
	return "agy"
}

func (a *AgyVoiceAdapter) InjectPrompt(sessionName, prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("prompt cannot be empty")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "--", trimmed, "Enter")
	return cmd.Run()
}

func (a *AgyVoiceAdapter) WaitForTurn(ctx context.Context, sessionName string, initialBuf []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	initialLen := len(initialBuf)
	var lastHash string
	quiescentCount := 0
	sawNewContent := false

	for {
		select {
		case <-ctx.Done():
			current, _ := CapturePaneLines(sessionName, initialLen+100)
			return extractDiffLines(initialBuf, current), nil
		case <-ticker.C:
			current, err := CapturePaneLines(sessionName, initialLen+100)
			if err != nil {
				continue
			}

			if len(current) > initialLen {
				sawNewContent = true
			}

			// Check for AGY prompt completion cues:
			// AGY is finished when output has stabilized and the bottom line shows the prompt cursor (> or similar)
			h := hashPaneLines(current)
			if h == lastHash && sawNewContent {
				quiescentCount++
				// Check bottom line to ensure agy isn't paused mid-tool
				bottomText := ""
				if len(current) > 0 {
					bottomText = strings.TrimSpace(current[len(current)-1])
				}
				isPromptReady := strings.HasPrefix(bottomText, ">") || strings.HasPrefix(bottomText, "?") || strings.HasPrefix(bottomText, "❯")

				// Quiescent for ~1.75s (5 ticks) or prompt is ready after 3 ticks (~1.0s)
				if (isPromptReady && quiescentCount >= 3) || quiescentCount >= 5 {
					diff := extractDiffLines(initialBuf, current)
					if strings.TrimSpace(diff) != "" {
						return diff, nil
					}
				}
			} else {
				lastHash = h
				quiescentCount = 0
			}
		}
	}
}

func (a *AgyVoiceAdapter) SanitizeSpokenResponse(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	// 1. Strip ANSI escapes
	text := ansiRegex.ReplaceAllString(raw, "")

	// 2. Replace markdown file links [file.go](file:///...) with just "file.go"
	text = fileLinkRegex.ReplaceAllString(text, "$1")
	text = plainFileRegex.ReplaceAllString(text, "file link")
	text = urlRegex.ReplaceAllString(text, "web link")

	// 3. Compress Markdown code blocks
	text = codeFenceRegex.ReplaceAllString(text, " I updated the code block. ")

	// 4. Clean line by line
	lines := strings.Split(text, "\n")
	var cleanLines []string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}

		// Skip echoed command prompts or spinners
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "$") || strings.HasPrefix(trimmed, "?") {
			continue
		}

		// Skip AGY tool execution / thinking announcements
		if agyToolRegex.MatchString(trimmed) {
			continue
		}

		// Remove spinner artifacts and symbols
		cleaned := spinnerChars.ReplaceAllString(trimmed, "")
		cleaned = multipleSpaces.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)

		// Filter out pure separator lines
		if strings.Trim(cleaned, "-=_*#`") == "" {
			continue
		}

		if cleaned != "" {
			cleanLines = append(cleanLines, cleaned)
		}
	}

	joined := strings.Join(cleanLines, " ")
	joined = multipleSpaces.ReplaceAllString(joined, " ")
	joined = strings.TrimSpace(joined)

	// If excessively long, speak the first 2-3 sentences
	if len(joined) > 340 {
		sentences := strings.SplitAfter(joined, ". ")
		if len(sentences) >= 2 {
			joined = strings.TrimSpace(sentences[0] + sentences[1])
		} else {
			joined = joined[:330] + "..."
		}
	}

	return joined
}

// --- CodexVoiceAdapter ---

type CodexVoiceAdapter struct{}

func (c *CodexVoiceAdapter) Name() string {
	return "codex"
}

func (c *CodexVoiceAdapter) InjectPrompt(sessionName, prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("prompt cannot be empty")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "--", trimmed, "Enter")
	return cmd.Run()
}

func (c *CodexVoiceAdapter) WaitForTurn(ctx context.Context, sessionName string, initialBuf []string, timeout time.Duration) (string, error) {
	if timeout <= 0 {
		timeout = 90 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(350 * time.Millisecond)
	defer ticker.Stop()

	initialLen := len(initialBuf)
	var lastHash string
	quiescentCount := 0
	sawNewContent := false

	for {
		select {
		case <-ctx.Done():
			current, _ := CapturePaneLines(sessionName, initialLen+80)
			return extractDiffLines(initialBuf, current), nil
		case <-ticker.C:
			current, err := CapturePaneLines(sessionName, initialLen+80)
			if err != nil {
				continue
			}

			// Check for 429 quota exhaustion immediately
			joinedOutput := strings.Join(current, " ")
			if strings.Contains(joinedOutput, "usage_limit_exceeded") || strings.Contains(joinedOutput, "You've hit your usage limit") || strings.Contains(joinedOutput, "rate_limit_exceeded") {
				return "OpenAI Codex usage limit reached. Your token quota has been exhausted. Please switch to Google Antigravity with key W.", nil
			}

			if len(current) > initialLen {
				sawNewContent = true
			}

			h := hashPaneLines(current)
			if h == lastHash && sawNewContent {
				quiescentCount++
				if quiescentCount >= 4 {
					diff := extractDiffLines(initialBuf, current)
					if strings.TrimSpace(diff) != "" {
						return diff, nil
					}
				}
			} else {
				lastHash = h
				quiescentCount = 0
			}
		}
	}
}

func (c *CodexVoiceAdapter) SanitizeSpokenResponse(raw string) string {
	if strings.TrimSpace(raw) == "" {
		return ""
	}

	text := ansiRegex.ReplaceAllString(raw, "")
	text = urlRegex.ReplaceAllString(text, "web link")
	text = codeFenceRegex.ReplaceAllString(text, " I updated the code block. ")
	text = strings.ReplaceAll(text, "`", "")

	lines := strings.Split(text, "\n")
	var cleanLines []string

	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, ">") || strings.HasPrefix(trimmed, "❯") || strings.HasPrefix(trimmed, "$") {
			continue
		}
		cleaned := spinnerChars.ReplaceAllString(trimmed, "")
		cleaned = multipleSpaces.ReplaceAllString(cleaned, " ")
		cleaned = strings.TrimSpace(cleaned)
		if cleaned != "" {
			cleanLines = append(cleanLines, cleaned)
		}
	}

	joined := strings.Join(cleanLines, " ")
	joined = multipleSpaces.ReplaceAllString(joined, " ")
	joined = strings.TrimSpace(joined)

	if len(joined) > 340 {
		sentences := strings.SplitAfter(joined, ". ")
		if len(sentences) >= 2 {
			joined = strings.TrimSpace(sentences[0] + sentences[1])
		} else {
			joined = joined[:330] + "..."
		}
	}
	return joined
}

// --- ClaudeVoiceAdapter ---

type ClaudeVoiceAdapter struct{}

func (cl *ClaudeVoiceAdapter) Name() string {
	return "claude"
}

func (cl *ClaudeVoiceAdapter) InjectPrompt(sessionName, prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("prompt cannot be empty")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "--", trimmed, "Enter")
	return cmd.Run()
}

func (cl *ClaudeVoiceAdapter) WaitForTurn(ctx context.Context, sessionName string, initialBuf []string, timeout time.Duration) (string, error) {
	return (&CodexVoiceAdapter{}).WaitForTurn(ctx, sessionName, initialBuf, timeout)
}

func (cl *ClaudeVoiceAdapter) SanitizeSpokenResponse(raw string) string {
	return (&AgyVoiceAdapter{}).SanitizeSpokenResponse(raw)
}

// --- GenericVoiceAdapter (OpenCode, custom providers) ---

type GenericVoiceAdapter struct {
	toolName string
}

func (g *GenericVoiceAdapter) Name() string {
	if g.toolName != "" {
		return g.toolName
	}
	return "generic"
}

func (g *GenericVoiceAdapter) InjectPrompt(sessionName, prompt string) error {
	trimmed := strings.TrimSpace(prompt)
	if trimmed == "" {
		return fmt.Errorf("prompt cannot be empty")
	}
	cmd := exec.Command("tmux", "send-keys", "-t", sessionName, "--", trimmed, "Enter")
	return cmd.Run()
}

func (g *GenericVoiceAdapter) WaitForTurn(ctx context.Context, sessionName string, initialBuf []string, timeout time.Duration) (string, error) {
	return (&CodexVoiceAdapter{}).WaitForTurn(ctx, sessionName, initialBuf, timeout)
}

func (g *GenericVoiceAdapter) SanitizeSpokenResponse(raw string) string {
	return (&AgyVoiceAdapter{}).SanitizeSpokenResponse(raw)
}
