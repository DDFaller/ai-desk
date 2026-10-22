package deck

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rtk"
)

// SessionInspection holds comprehensive runtime telemetry for an agent session.
type SessionInspection struct {
	Instance        InstanceRecord
	Attached        bool
	Uptime          time.Duration
	ActiveProfile   string
	ActiveSkills    []string
	PromptTaxTurn   int
	TokensUsed      TokenUsage
	HasTokenMetrics bool
	RTKSavings      *rtk.SavingsSummary
	TerminalPreview []string
}

// InspectSession aggregates deep diagnostics for a given instance.
func (s *StateDB) InspectSession(sessionIDOrTitle string) (*SessionInspection, error) {
	// 1. Locate instance record
	instances, err := s.LoadInstances()
	if err != nil {
		return nil, err
	}

	var target *InstanceRecord
	for _, inst := range instances {
		if inst.ID == sessionIDOrTitle || inst.Title == sessionIDOrTitle {
			target = &inst
			break
		}
	}
	if target == nil {
		return nil, os.ErrNotExist
	}

	insp := &SessionInspection{
		Instance: *target,
	}

	if target.CreatedAt > 0 {
		insp.Uptime = time.Since(time.Unix(target.CreatedAt, 0))
	}

	// 2. Query tmux attached state
	if target.TmuxSession != "" {
		cmd := exec.Command("tmux", "display-message", "-p", "-t", target.TmuxSession, "#{session_attached}")
		if out, err := cmd.Output(); err == nil {
			insp.Attached = strings.TrimSpace(string(out)) == "1"
		}
	}

	// 3. Inspect Active Skills & Prompt Tax
	insp.ActiveProfile = harness.DetectWorkspaceProfile(target.ProjectPath)
	skillsCfg, _ := harness.LoadSkillProfiles("")
	if skillsCfg != nil {
		resolved, _ := skillsCfg.ResolveSkills(insp.ActiveProfile, "")
		insp.ActiveSkills = resolved
		tax := len(resolved) * 60
		if len(resolved) >= 70 {
			tax = 4500
		}
		insp.PromptTaxTurn = tax
	}

	// Also check if per-session sandbox has extra dynamically attached skills
	v := harness.NewVirtualizer("", "")
	accID := target.Account
	if accID == "" {
		accID = target.ID
	}
	attached, _ := v.ListAttachedSkills(target.Tool, accID)
	if len(attached) > 0 {
		for _, s := range attached {
			alreadyPresent := false
			for _, r := range insp.ActiveSkills {
				if r == s {
					alreadyPresent = true
					break
				}
			}
			if !alreadyPresent {
				insp.ActiveSkills = append(insp.ActiveSkills, s+" (dynamic)")
				insp.PromptTaxTurn += 60
			}
		}
	}

	// 4. Token metrics inspection
	insp.TokensUsed, insp.HasTokenMetrics = extractTokenMetrics(target)

	// 5. RTK Token Savings
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		if savings, err := rtkClient.GetSavings(); err == nil {
			insp.RTKSavings = savings
		}
	}

	// 6. Live non-blocking terminal preview (last 15 lines)
	if target.TmuxSession != "" {
		insp.TerminalPreview = captureTmuxPane(target.TmuxSession, 15)
	}

	return insp, nil
}

func captureTmuxPane(tmuxSession string, lines int) []string {
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "capture-pane", "-pt", tmuxSession, "-S", "-"+strconv.Itoa(lines))
	out, err := cmd.Output()
	if err != nil {
		return []string{"[Terminal preview unavailable: session idle or detached]"}
	}

	rawLines := strings.Split(strings.TrimRight(string(out), "\r\n"), "\n")
	cleanLines := make([]string, 0, len(rawLines))
	for _, l := range rawLines {
		cleanLines = append(cleanLines, strings.TrimRight(l, "\r\n"))
	}
	return cleanLines
}

// extractTokenMetrics searches transcripts/rollouts for token usage events.
func extractTokenMetrics(target *InstanceRecord) (TokenUsage, bool) {
	home, _ := os.UserHomeDir()

	// Try extracting codex session rollout
	if target.Tool == "codex" {
		var toolData map[string]interface{}
		_ = json.Unmarshal([]byte(target.ToolData), &toolData)
		codexID, _ := toolData["codex_session_id"].(string)

		sessionsDir := filepath.Join(home, ".codex", "sessions")
		if _, err := os.Stat(sessionsDir); err == nil {
			// Find most recent rollout file mentioning codexID or recent date
			var latestFile string
			var latestMod time.Time
			_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
					if codexID != "" && strings.Contains(filepath.Base(path), codexID) {
						latestFile = path
						return filepath.SkipAll
					}
					if info.ModTime().After(latestMod) {
						latestMod = info.ModTime()
						latestFile = path
					}
				}
				return nil
			})

			if latestFile != "" {
				if usage, ok := parseCodexRolloutTokens(latestFile); ok {
					return usage, true
				}
			}
		}
	}

	// Try Claude Code transcripts
	if target.Tool == "claude" {
		claudeDir := filepath.Join(home, ".claude", "projects")
		if _, err := os.Stat(claudeDir); err == nil {
			var latestFile string
			var latestMod time.Time
			_ = filepath.Walk(claudeDir, func(path string, info os.FileInfo, err error) error {
				if err == nil && !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
					if info.ModTime().After(latestMod) {
						latestMod = info.ModTime()
						latestFile = path
					}
				}
				return nil
			})
			if latestFile != "" {
				if usage, ok := parseClaudeTranscriptTokens(latestFile); ok {
					return usage, true
				}
			}
		}
	}

	return TokenUsage{}, false
}

func parseCodexRolloutTokens(path string) (TokenUsage, bool) {
	f, err := os.Open(path)
	if err != nil {
		return TokenUsage{}, false
	}
	defer f.Close()

	var lastUsage TokenUsage
	found := false

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), "token_count") {
			continue
		}

		var ev struct {
			Type    string `json:"type"`
			Payload struct {
				Type string `json:"type"`
				Info struct {
					TotalTokenUsage struct {
						InputTokens        int64 `json:"input_tokens"`
						CachedInputTokens  int64 `json:"cached_input_tokens"`
						OutputTokens       int64 `json:"output_tokens"`
						ReasoningTokens    int64 `json:"reasoning_output_tokens"`
						TotalTokens        int64 `json:"total_tokens"`
					} `json:"total_token_usage"`
					ModelContextWindow int64 `json:"model_context_window"`
				} `json:"info"`
			} `json:"payload"`
		}

		if err := json.Unmarshal(line, &ev); err == nil && ev.Payload.Type == "token_count" {
			info := ev.Payload.Info.TotalTokenUsage
			lastUsage = TokenUsage{
				TotalTokens:        info.TotalTokens,
				InputTokens:        info.InputTokens,
				OutputTokens:       info.OutputTokens,
				CachedInputTokens:  info.CachedInputTokens,
				ReasoningTokens:    info.ReasoningTokens,
				ModelContextWindow: ev.Payload.Info.ModelContextWindow,
			}
			found = true
		}
	}
	return lastUsage, found
}

func parseClaudeTranscriptTokens(path string) (TokenUsage, bool) {
	f, err := os.Open(path)
	if err != nil {
		return TokenUsage{}, false
	}
	defer f.Close()

	var usage TokenUsage
	found := false

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"usage"`) {
			continue
		}

		var entry struct {
			Usage struct {
				InputTokens       int64 `json:"input_tokens"`
				OutputTokens      int64 `json:"output_tokens"`
				CacheReadTokens   int64 `json:"cache_read_input_tokens"`
				CacheWriteTokens  int64 `json:"cache_creation_input_tokens"`
			} `json:"usage"`
		}

		if err := json.Unmarshal(line, &entry); err == nil && entry.Usage.InputTokens > 0 {
			usage.InputTokens += entry.Usage.InputTokens
			usage.OutputTokens += entry.Usage.OutputTokens
			usage.CachedInputTokens += entry.Usage.CacheReadTokens
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			found = true
		}
	}
	return usage, found
}
