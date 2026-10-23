package deck

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// HandoffBrief encapsulates the structured context transferred across agent harnesses.
type HandoffBrief struct {
	SessionID    string
	Title        string
	ProjectPath  string
	SourceTool   string
	TargetTool   string
	GitBranch    string
	GitStatus    string
	GitDiffStat  string
	TerminalTail string
	CustomPrompt string
	Timestamp    time.Time
}

// CaptureTmuxBuffer captures the most recent visible output lines from a running tmux session.
func CaptureTmuxBuffer(tmuxSession string, maxLines int) (string, error) {
	if tmuxSession == "" {
		return "", fmt.Errorf("tmux session name is empty")
	}
	if maxLines <= 0 {
		maxLines = 200
	}

	cmd := exec.Command("tmux", "capture-pane", "-p", "-S", fmt.Sprintf("-%d", maxLines), "-t", tmuxSession)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("tmux capture-pane for %s: %w", tmuxSession, err)
	}

	lines := strings.Split(string(out), "\n")
	// Trim trailing blank lines
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}

	// Limit to maxLines
	if len(lines) > maxLines {
		lines = lines[len(lines)-maxLines:]
	}

	return strings.Join(lines, "\n"), nil
}

// SanitizeTerminalCapture strips out TUI box-drawing artifacts, status bars, and terminal garbage.
func SanitizeTerminalCapture(raw string) string {
	if raw == "" {
		return ""
	}
	lines := strings.Split(raw, "\n")
	var clean []string
	for _, l := range lines {
		trimmed := strings.TrimSpace(l)
		if trimmed == "" {
			continue
		}
		// Strip status lines and TUI control artifacts
		if strings.Contains(l, "ctrl + t to view transcript") ||
			strings.Contains(l, "› Ask Codex to do anything") ||
			strings.Contains(l, "Conversation interrupted") ||
			strings.Contains(l, "Hit `/feedback` to report") ||
			strings.Contains(l, "gpt-6-astra") ||
			strings.Contains(l, "claude-3-7-sonnet") {
			continue
		}
		// Strip box drawing border lines like ━━━━━━━━ or ─────────
		if strings.Trim(trimmed, "─━=-_") == "" && len(trimmed) > 3 {
			continue
		}
		// Clean up leading box-drawing prefixes
		cleanLine := strings.TrimPrefix(l, "  └ ")
		cleanLine = strings.TrimPrefix(cleanLine, "  │ ")
		cleanLine = strings.TrimPrefix(cleanLine, "• ")
		cleanLine = strings.TrimPrefix(cleanLine, "› ")
		cleanLine = strings.TrimSpace(cleanLine)
		if cleanLine != "" {
			clean = append(clean, cleanLine)
		}
	}
	// Limit to the most recent 50 meaningful lines
	if len(clean) > 50 {
		clean = clean[len(clean)-50:]
	}
	return strings.Join(clean, "\n")
}

// ExtractStructuredConversation attempts to extract clean user & assistant dialogue
// from the source tool's native session logs.
func ExtractStructuredConversation(inst *InstanceRecord) string {
	if inst == nil {
		return ""
	}
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}

	toolData := make(map[string]interface{})
	if inst.ToolData != "" {
		_ = json.Unmarshal([]byte(inst.ToolData), &toolData)
	}

	tool := strings.ToLower(inst.Tool)
	switch tool {
	case "codex":
		sessionID, _ := toolData["codex_session_id"].(string)
		if sessionID == "" {
			return ""
		}
		sessionsDir := filepath.Join(home, ".codex", "sessions")
		var matchedFile string
		_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
			if err != nil || matchedFile != "" {
				return nil
			}
			if !info.IsDir() && strings.Contains(info.Name(), sessionID) && strings.HasSuffix(info.Name(), ".jsonl") {
				matchedFile = path
				return filepath.SkipAll
			}
			return nil
		})
		if matchedFile == "" {
			return ""
		}
		data, err := os.ReadFile(matchedFile)
		if err != nil {
			return ""
		}
		lines := strings.Split(string(data), "\n")
		type turn struct {
			role string
			text string
		}
		var turns []turn
		for _, line := range lines {
			if strings.TrimSpace(line) == "" {
				continue
			}
			var obj map[string]interface{}
			if json.Unmarshal([]byte(line), &obj) != nil {
				continue
			}
			if obj["type"] != "response_item" {
				continue
			}
			payload, _ := obj["payload"].(map[string]interface{})
			if payload == nil || payload["type"] != "message" {
				continue
			}
			role, _ := payload["role"].(string)
			if role != "user" && role != "assistant" {
				continue
			}
			contentArr, _ := payload["content"].([]interface{})
			var textParts []string
			for _, item := range contentArr {
				if m, ok := item.(map[string]interface{}); ok {
					if t, ok := m["text"].(string); ok {
						if strings.HasPrefix(t, "<environment_context>") || strings.HasPrefix(t, "<skills_instructions>") {
							continue
						}
						textParts = append(textParts, strings.TrimSpace(t))
					}
				}
			}
			combined := strings.Join(textParts, "\n\n")
			if strings.TrimSpace(combined) != "" {
				turns = append(turns, turn{role: role, text: combined})
			}
		}

		if len(turns) == 0 {
			return ""
		}
		// Keep last up to 6 turns
		if len(turns) > 6 {
			turns = turns[len(turns)-6:]
		}
		var sb strings.Builder
		for _, t := range turns {
			sb.WriteString(fmt.Sprintf("[%s]\n%s\n\n", strings.ToUpper(t.role), t.text))
		}
		return strings.TrimSpace(sb.String())
	}
	return ""
}

// CaptureGitDelta inspects the project directory and returns git branch, status, and diff stat.
func CaptureGitDelta(projectPath string) (branch string, status string, diffStat string, err error) {
	if projectPath == "" {
		return "", "", "", nil
	}

	// 1. Check if git repository
	if _, statErr := os.Stat(filepath.Join(projectPath, ".git")); statErr != nil {
		// Try parent or check git rev-parse
		chk := exec.Command("git", "-C", projectPath, "rev-parse", "--is-inside-work-tree")
		if chkErr := chk.Run(); chkErr != nil {
			return "", "", "", nil // Not a git repository
		}
	}

	// 2. Branch & commit
	bOut, _ := exec.Command("git", "-C", projectPath, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch = strings.TrimSpace(string(bOut))

	cOut, _ := exec.Command("git", "-C", projectPath, "rev-parse", "--short", "HEAD").Output()
	shortCommit := strings.TrimSpace(string(cOut))
	if shortCommit != "" {
		branch = fmt.Sprintf("%s (%s)", branch, shortCommit)
	}

	// 3. Status
	sOut, _ := exec.Command("git", "-C", projectPath, "status", "-s").Output()
	status = strings.TrimSpace(string(sOut))

	// 4. Diff stat
	dOut, _ := exec.Command("git", "-C", projectPath, "diff", "--stat", "HEAD").Output()
	diffStat = strings.TrimSpace(string(dOut))

	return branch, status, diffStat, nil
}

// BuildHandoffBrief constructs a human and LLM-friendly briefing document.
func BuildHandoffBrief(brief *HandoffBrief) string {
	if brief.Timestamp.IsZero() {
		brief.Timestamp = time.Now()
	}

	var sb strings.Builder
	sb.WriteString("=== CROSS-HARNESS SESSION HANDOFF ===\n\n")
	sb.WriteString(fmt.Sprintf("• Session:          %s (%s)\n", brief.Title, brief.SessionID))
	sb.WriteString(fmt.Sprintf("• Original Harness: %s\n", brief.SourceTool))
	sb.WriteString(fmt.Sprintf("• Target Harness:   %s\n", brief.TargetTool))
	sb.WriteString(fmt.Sprintf("• Working Dir:      %s\n", brief.ProjectPath))
	sb.WriteString(fmt.Sprintf("• Timestamp:        %s\n", brief.Timestamp.Format("2006-01-02 15:04:05 MST")))

	if brief.GitBranch != "" {
		sb.WriteString(fmt.Sprintf("• Git Branch:       %s\n", brief.GitBranch))
	}

	if brief.GitStatus != "" {
		sb.WriteString("\n--- UNCOMMITTED WORKSPACE MODIFICATIONS ---\n")
		sb.WriteString(brief.GitStatus)
		sb.WriteString("\n")
	}

	if brief.GitDiffStat != "" {
		sb.WriteString("\n--- GIT DIFF STAT ---\n")
		sb.WriteString(brief.GitDiffStat)
		sb.WriteString("\n")
	}

	if brief.CustomPrompt != "" {
		sb.WriteString("\n--- OPERATOR DIRECTIVES FOR NEW HARNESS ---\n")
		sb.WriteString(brief.CustomPrompt)
		sb.WriteString("\n")
	}

	if brief.TerminalTail != "" {
		sb.WriteString("\n--- RECENT TERMINAL CONVERSATION TAIL (LAST TURNS) ---\n")
		sb.WriteString(brief.TerminalTail)
		sb.WriteString("\n--- END RECENT CONVERSATION TAIL ---\n")
	}

	sb.WriteString(fmt.Sprintf(`
You are continuing this task as %s, taking over from the prior %s agent.
Review the workspace state, modified files, and recent dialogue above.
Acknowledge the handoff by saying "HANDOFF RECEIVED" and proceed with the task.
`, brief.TargetTool, brief.SourceTool))

	return sb.String()
}

// SaveHandoffBrief persists the handoff document to ~/.ai-desk/handoffs/ or project dir.
func SaveHandoffBrief(projectPath, sessionID, content string) (string, error) {
	var handoffsDir string
	if custom := os.Getenv("AI_DESK_HOME"); custom != "" {
		handoffsDir = filepath.Join(custom, "handoffs")
	} else if home, err := os.UserHomeDir(); err == nil {
		handoffsDir = filepath.Join(home, ".ai-desk", "handoffs")
	} else {
		handoffsDir = filepath.Join(os.TempDir(), "ai-desk", "handoffs")
	}

	if err := os.MkdirAll(handoffsDir, 0700); err != nil {
		// Fallback to project path or temp dir if home is read-only
		if projectPath != "" {
			handoffsDir = filepath.Join(projectPath, ".ai-desk")
			_ = os.MkdirAll(handoffsDir, 0700)
		} else {
			handoffsDir = filepath.Join(os.TempDir(), "ai-desk", "handoffs")
			_ = os.MkdirAll(handoffsDir, 0700)
		}
	}

	targetPath := filepath.Join(handoffsDir, fmt.Sprintf("%s-handoff.md", sessionID))
	if err := os.WriteFile(targetPath, []byte(content), 0600); err != nil {
		// Fallback to writing directly in project dir if possible
		if projectPath != "" {
			altPath := filepath.Join(projectPath, fmt.Sprintf("%s-handoff.md", sessionID))
			if altErr := os.WriteFile(altPath, []byte(content), 0600); altErr == nil {
				_ = os.WriteFile(filepath.Join(projectPath, ".ai-desk-handoff.md"), []byte(content), 0600)
				return altPath, nil
			}
		}
		return "", fmt.Errorf("write handoff brief: %w", err)
	}

	// Also write in project path if writable
	if projectPath != "" && projectPath != handoffsDir {
		projFile := filepath.Join(projectPath, ".ai-desk-handoff.md")
		_ = os.WriteFile(projFile, []byte(content), 0600)
	}

	return targetPath, nil
}
