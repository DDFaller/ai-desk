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
	"sync"
	"time"

	"github.com/falluba/ai-desk/pkg/configpaths"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/rtk"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

type inspectionCacheEntry struct {
	inspection *SessionInspection
	cachedAt   time.Time
}

type tokenMetricsCacheEntry struct {
	usage    TokenUsage
	found    bool
	cachedAt time.Time
}

var (
	inspectionCache     = make(map[string]inspectionCacheEntry)
	inspectionCacheMu   sync.RWMutex
	tokenMetricsCache   = make(map[string]tokenMetricsCacheEntry)
	tokenMetricsCacheMu sync.RWMutex

	agyAccountsCache       []rotation.AccountStatus
	agyAccountsCachedAt    time.Time
	agyAccountsCacheMu     sync.RWMutex
	agyBrainTokensCache    TokenUsage
	agyBrainTokensCachedAt time.Time
	agyBrainTokensCacheMu  sync.RWMutex
)

func getCachedAgyAccounts() []rotation.AccountStatus {
	agyAccountsCacheMu.RLock()
	if len(agyAccountsCache) > 0 && time.Since(agyAccountsCachedAt) < 10*time.Second {
		accs := agyAccountsCache
		agyAccountsCacheMu.RUnlock()
		return accs
	}
	agyAccountsCacheMu.RUnlock()

	sb := swapbridge.NewSwapBridge("")
	accounts, err := sb.LoadAccounts()
	if err == nil && len(accounts) > 0 {
		agyAccountsCacheMu.Lock()
		agyAccountsCache = accounts
		agyAccountsCachedAt = time.Now()
		agyAccountsCacheMu.Unlock()
	}
	return accounts
}

// InvalidateInspectionCache evicts a session or all sessions from the telemetry cache.
func InvalidateInspectionCache(sessionID string) {
	inspectionCacheMu.Lock()
	if sessionID == "" {
		inspectionCache = make(map[string]inspectionCacheEntry)
	} else {
		delete(inspectionCache, sessionID)
		for k, v := range inspectionCache {
			if v.inspection != nil && (v.inspection.Instance.ID == sessionID || v.inspection.Instance.Title == sessionID) {
				delete(inspectionCache, k)
			}
		}
	}
	inspectionCacheMu.Unlock()

	tokenMetricsCacheMu.Lock()
	if sessionID == "" {
		tokenMetricsCache = make(map[string]tokenMetricsCacheEntry)
	} else {
		for k := range tokenMetricsCache {
			if strings.HasSuffix(k, ":"+sessionID) || strings.Contains(k, sessionID) {
				delete(tokenMetricsCache, k)
			}
		}
	}
	tokenMetricsCacheMu.Unlock()
}

// ResolvedAccount represents the verified account identity for a session/task.
type ResolvedAccount struct {
	Email       string `json:"email"`        // Verified email (e.g. "danielmcf15@gmail.com")
	DisplayName string `json:"display_name"` // Human-readable badge (e.g. "danielmcf15@gmail.com" or "clownfishexperience@gmail.com (default)")
	IsDefault   bool   `json:"is_default"`   // True if inherited from default configuration
	Source      string `json:"source"`       // "db", "tmux", "sandbox", "default_config", or "vault"
}

// SessionInspection holds comprehensive runtime telemetry for an agent session.
type SessionInspection struct {
	Instance        InstanceRecord
	ResolvedAccount ResolvedAccount
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

// GetCachedInspection returns a fresh cached inspection if available.
func GetCachedInspection(sessionIDOrTitle string) *SessionInspection {
	inspectionCacheMu.RLock()
	defer inspectionCacheMu.RUnlock()
	if entry, ok := inspectionCache[sessionIDOrTitle]; ok {
		if time.Since(entry.cachedAt) < 15*time.Second {
			return entry.inspection
		}
	}
	return nil
}

// FastInspectionFromRecord creates an immediate in-memory inspection from a loaded InstanceRecord (0ms latency).
func FastInspectionFromRecord(inst *InstanceRecord) *SessionInspection {
	if inst == nil {
		return nil
	}
	insp := &SessionInspection{
		Instance:        *inst,
		ResolvedAccount: ResolveInstanceAccount(inst),
	}
	if inst.CreatedAt > 0 {
		insp.Uptime = time.Since(time.Unix(inst.CreatedAt, 0))
	}
	if inst.ToolData != "" {
		var td map[string]interface{}
		if err := json.Unmarshal([]byte(inst.ToolData), &td); err == nil {
			if sp, ok := td["skill_profile"].(string); ok && sp != "" {
				insp.ActiveProfile = sp
			}
		}
	}
	if insp.ActiveProfile == "" {
		insp.ActiveProfile = "core"
	}
	insp.TokensUsed, insp.HasTokenMetrics = extractTokenMetrics(inst)
	return insp
}

// InspectSessionRecord performs deep inspection for an already loaded InstanceRecord without SQLite table scan.
func (s *StateDB) InspectSessionRecord(target *InstanceRecord) (*SessionInspection, error) {
	if target == nil {
		return nil, os.ErrNotExist
	}

	// Check memory cache first
	inspectionCacheMu.RLock()
	if entry, ok := inspectionCache[target.ID]; ok {
		if time.Since(entry.cachedAt) < 15*time.Second {
			inspectionCacheMu.RUnlock()
			return entry.inspection, nil
		}
	}
	inspectionCacheMu.RUnlock()

	insp := &SessionInspection{
		Instance:        *target,
		ResolvedAccount: ResolveInstanceAccount(target),
	}

	if target.CreatedAt > 0 {
		insp.Uptime = time.Since(time.Unix(target.CreatedAt, 0))
	}

	// 1. Query tmux attached state
	if target.TmuxSession != "" {
		ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
		cmd := exec.CommandContext(ctx, "tmux", "display-message", "-p", "-t", target.TmuxSession, "#{session_attached}")
		if out, err := cmd.Output(); err == nil {
			insp.Attached = strings.TrimSpace(string(out)) == "1"
		}
		cancel()
	}

	// 2. Inspect Active Skills & Prompt Tax
	insp.ActiveProfile = ""
	if target.ToolData != "" {
		var td map[string]interface{}
		if err := json.Unmarshal([]byte(target.ToolData), &td); err == nil {
			if sp, ok := td["skill_profile"].(string); ok && sp != "" {
				insp.ActiveProfile = sp
			}
		}
	}
	if insp.ActiveProfile == "" {
		insp.ActiveProfile = harness.DetectWorkspaceProfile(target.ProjectPath)
	}
	v := harness.NewVirtualizer("", "")
	insp.ActiveSkills, _ = SessionSkills(v, target)
	insp.PromptTaxTurn = len(insp.ActiveSkills) * 60
	if cfg, err := ReadSessionConfig(target); err == nil && cfg.SkillsPending {
		insp.ActiveProfile += " (restart required)"
	}

	// 3. Token metrics inspection
	insp.TokensUsed, insp.HasTokenMetrics = extractTokenMetrics(target)

	// 4. RTK Token Savings
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		if savings, err := rtkClient.GetSavings(); err == nil {
			insp.RTKSavings = savings
		}
	}

	// 5. Live non-blocking terminal preview (last 15 lines)
	if target.TmuxSession != "" {
		insp.TerminalPreview = captureTmuxPane(target.TmuxSession, 15)
	}

	// Store in fast memory cache
	inspectionCacheMu.Lock()
	inspectionCache[target.ID] = inspectionCacheEntry{inspection: insp, cachedAt: time.Now()}
	inspectionCache[target.Title] = inspectionCacheEntry{inspection: insp, cachedAt: time.Now()}
	inspectionCacheMu.Unlock()

	return insp, nil
}

// InspectSession aggregates deep diagnostics for a given instance.
func (s *StateDB) InspectSession(sessionIDOrTitle string) (*SessionInspection, error) {
	// Check memory cache first
	inspectionCacheMu.RLock()
	if entry, ok := inspectionCache[sessionIDOrTitle]; ok {
		if time.Since(entry.cachedAt) < 15*time.Second {
			inspectionCacheMu.RUnlock()
			return entry.inspection, nil
		}
	}
	inspectionCacheMu.RUnlock()

	// 1. Locate instance record via single-row query or fallback
	target, err := s.FindInstance(sessionIDOrTitle)
	if err != nil {
		instances, loadErr := s.LoadInstances()
		if loadErr != nil {
			return nil, loadErr
		}
		for _, inst := range instances {
			if inst.ID == sessionIDOrTitle || inst.Title == sessionIDOrTitle {
				target = &inst
				break
			}
		}
		if target == nil {
			return nil, os.ErrNotExist
		}
	}

	return s.InspectSessionRecord(target)
}

func captureTmuxPane(tmuxSession string, lines int) []string {
	if tmuxSession == "" {
		return []string{"[Terminal preview unavailable: session idle or detached]"}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
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

// ResolveInstanceAccount dynamically investigates and resolves the true bound account for an instance.
func ResolveInstanceAccount(inst *InstanceRecord) ResolvedAccount {
	if inst == nil {
		return ResolvedAccount{DisplayName: "none", IsDefault: true, Source: "none"}
	}

	trimmedAcc := strings.TrimSpace(inst.Account)

	// 1. If explicit email in state.db
	if trimmedAcc != "" && trimmedAcc != "default" {
		if strings.Contains(trimmedAcc, "@") {
			return ResolvedAccount{
				Email:       trimmedAcc,
				DisplayName: trimmedAcc,
				IsDefault:   false,
				Source:      "db",
			}
		}
		// Match against vault accounts
		for _, a := range getCachedAgyAccounts() {
			if strings.EqualFold(a.ID, trimmedAcc) || strings.EqualFold(a.Email, trimmedAcc) {
				return ResolvedAccount{
					Email:       a.Email,
					DisplayName: a.Email,
					IsDefault:   false,
					Source:      "db",
				}
			}
		}
		return ResolvedAccount{
			Email:       trimmedAcc,
			DisplayName: trimmedAcc,
			IsDefault:   false,
			Source:      "db",
		}
	}

	// 2. If running inside tmux, inspect tmux pane environment
	if inst.TmuxSession != "" {
		if tmuxAcc := queryTmuxEnv(inst.TmuxSession, "AI_DESK_ACCOUNT"); tmuxAcc != "" && tmuxAcc != "default" && strings.Contains(tmuxAcc, "@") {
			return ResolvedAccount{
				Email:       tmuxAcc,
				DisplayName: tmuxAcc,
				IsDefault:   false,
				Source:      "tmux",
			}
		}
		if cliHome := queryTmuxEnv(inst.TmuxSession, "GEMINI_CLI_HOME"); cliHome != "" {
			if email := readActiveAccountHint(cliHome); email != "" {
				return ResolvedAccount{
					Email:       email,
					DisplayName: email,
					IsDefault:   false,
					Source:      "sandbox",
				}
			}
		}
		if homeDir := queryTmuxEnv(inst.TmuxSession, "HOME"); homeDir != "" && strings.Contains(homeDir, ".ai-desk/harnesses") {
			if email := readActiveAccountHint(homeDir); email != "" {
				return ResolvedAccount{
					Email:       email,
					DisplayName: email,
					IsDefault:   false,
					Source:      "sandbox",
				}
			}
		}
	}

	// 3. For agy/gemini, check default host configuration
	if inst.Tool == "agy" || inst.Tool == "gemini" || inst.Tool == "" {
		if defEmail := readDefaultGoogleAccount(); defEmail != "" {
			return ResolvedAccount{
				Email:       defEmail,
				DisplayName: defEmail + " (default)",
				IsDefault:   true,
				Source:      "default_config",
			}
		}

		// Fallback to first vault account if available
		vaultAccs := getCachedAgyAccounts()
		if len(vaultAccs) > 0 {
			return ResolvedAccount{
				Email:       vaultAccs[0].Email,
				DisplayName: vaultAccs[0].Email + " (vault)",
				IsDefault:   true,
				Source:      "vault",
			}
		}
	}

	// 4. Default fallback for non-agy or unconfigured tools
	display := trimmedAcc
	if display == "" {
		display = "default"
	}
	return ResolvedAccount{
		Email:       "",
		DisplayName: display,
		IsDefault:   true,
		Source:      "default",
	}
}

func queryTmuxEnv(tmuxSession, varName string) string {
	if tmuxSession == "" || varName == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	cmd := exec.CommandContext(ctx, "tmux", "show-environment", "-t", tmuxSession, varName)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(out))
	if strings.HasPrefix(line, varName+"=") {
		return strings.TrimPrefix(line, varName+"=")
	}
	return ""
}

func readActiveAccountHint(dir string) string {
	if dir == "" {
		return ""
	}
	hintPath := filepath.Join(dir, "active_account.json")
	if data, err := os.ReadFile(hintPath); err == nil {
		var m map[string]string
		if err := json.Unmarshal(data, &m); err == nil {
			if email, ok := m["email"]; ok && strings.Contains(email, "@") {
				return email
			}
		}
	}
	geminiAccPath := filepath.Join(dir, ".gemini", "google_accounts.json")
	if data, err := os.ReadFile(geminiAccPath); err == nil {
		var m struct {
			Active string `json:"active"`
		}
		if err := json.Unmarshal(data, &m); err == nil {
			if strings.Contains(m.Active, "@") {
				return m.Active
			}
		}
	}
	base := filepath.Base(dir)
	if strings.Contains(base, "_") && (strings.HasSuffix(base, ".com") || strings.HasSuffix(base, ".org") || strings.HasSuffix(base, ".net")) {
		candidate := strings.Replace(base, "_", "@", 1)
		if strings.Contains(candidate, "@") {
			return candidate
		}
	}
	return ""
}

func readDefaultGoogleAccount() string {
	home, err := configpaths.HomeDir()
	if err != nil {
		return ""
	}
	p := filepath.Join(home, ".gemini", "google_accounts.json")
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	var acc struct {
		Active string `json:"active"`
	}
	if err := json.Unmarshal(data, &acc); err == nil {
		if strings.Contains(acc.Active, "@") {
			return acc.Active
		}
	}
	return ""
}

// extractTokenMetrics searches transcripts/rollouts for token usage events.
func extractTokenMetrics(target *InstanceRecord) (TokenUsage, bool) {
	if target == nil {
		return TokenUsage{}, false
	}
	cfg, _ := ReadSessionConfig(target)
	cacheKey := target.Tool + ":" + target.Account + ":" + target.ID + ":" + cfg.SandboxID
	tokenMetricsCacheMu.RLock()
	if entry, ok := tokenMetricsCache[cacheKey]; ok {
		if time.Since(entry.cachedAt) < 5*time.Second {
			tokenMetricsCacheMu.RUnlock()
			return entry.usage, entry.found
		}
	}
	tokenMetricsCacheMu.RUnlock()

	usage, found := extractTokenMetricsInternal(target)

	tokenMetricsCacheMu.Lock()
	tokenMetricsCache[cacheKey] = tokenMetricsCacheEntry{usage: usage, found: found, cachedAt: time.Now()}
	tokenMetricsCacheMu.Unlock()

	return usage, found
}

func extractTokenMetricsInternal(target *InstanceRecord) (TokenUsage, bool) {
	home, _ := configpaths.HomeDir()

	// 1. Try extracting AGY quotas and transcripts
	if target.Tool == "agy" || target.Tool == "gemini" {
		accounts := getCachedAgyAccounts()
		resAcc := ResolveInstanceAccount(target)
		targetEmail := resAcc.Email

		var activeAcc *rotation.AccountStatus
		if len(accounts) > 0 {
			for i := range accounts {
				if targetEmail != "" && strings.EqualFold(accounts[i].Email, targetEmail) {
					activeAcc = &accounts[i]
					break
				}
			}
			if activeAcc == nil && targetEmail != "" {
				for i := range accounts {
					if strings.HasPrefix(strings.ToLower(accounts[i].Email), strings.ToLower(targetEmail)) {
						activeAcc = &accounts[i]
						break
					}
				}
			}
		}

		if activeAcc != nil {
			usage := TokenUsage{
				RateLimitUsedPct: 100.0 - activeAcc.RemainingQuotaPct,
				AccountEmail:     resAcc.DisplayName,
				PlanType:         "Google Antigravity",
			}
			if activeAcc.RemainingQuotaPct <= 0.0 {
				usage.Exhausted = true
				usage.ExhaustionReason = "Antigravity account quota depleted"
			}
			_ = parseAgyTranscriptTokens(&usage)
			return usage, true
		} else if targetEmail != "" {
			usage := TokenUsage{
				AccountEmail: resAcc.DisplayName,
				PlanType:     "Google Antigravity",
			}
			_ = parseAgyTranscriptTokens(&usage)
			return usage, true
		}
	}

	// 2. Try extracting codex session rollout
	if target.Tool == "codex" {
		var toolData map[string]interface{}
		_ = json.Unmarshal([]byte(target.ToolData), &toolData)
		codexID, _ := toolData["codex_session_id"].(string)

		sessionsDir := filepath.Join(home, ".codex", "sessions")
		if cfg, _ := ReadSessionConfig(target); cfg.Version >= 1 {
			sessionsDir = filepath.Join(SessionVirtualizer(harness.NewVirtualizer("", ""), target).SandboxPath(target.Tool, target.Account), "sessions")
		}
		if _, err := os.Stat(sessionsDir); err == nil {
			var latestFile string
			var latestMod time.Time
			baseDepth := strings.Count(sessionsDir, string(os.PathSeparator))
			_ = filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() && strings.Count(path, string(os.PathSeparator))-baseDepth > 4 {
					return filepath.SkipDir
				}
				if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
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

	// 3. Try Claude Code transcripts
	if target.Tool == "claude" {
		claudeDir := filepath.Join(home, ".claude", "projects")
		if cfg, _ := ReadSessionConfig(target); cfg.Version >= 1 {
			claudeDir = filepath.Join(SessionVirtualizer(harness.NewVirtualizer("", ""), target).SandboxPath(target.Tool, target.Account), "projects")
		}
		if _, err := os.Stat(claudeDir); err == nil {
			var latestFile string
			var latestMod time.Time
			baseDepth := strings.Count(claudeDir, string(os.PathSeparator))
			_ = filepath.Walk(claudeDir, func(path string, info os.FileInfo, err error) error {
				if err != nil {
					return nil
				}
				if info.IsDir() && strings.Count(path, string(os.PathSeparator))-baseDepth > 3 {
					return filepath.SkipDir
				}
				if !info.IsDir() && strings.HasSuffix(path, ".jsonl") {
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
		lineStr := string(line)

		if reason := quotaEvent(line); reason != "" {
			lastUsage.Exhausted = true
			lastUsage.ExhaustionReason = reason
			found = true
		}

		if strings.Contains(lineStr, "token_count") {
			var ev struct {
				Type    string `json:"type"`
				Payload struct {
					Type string `json:"type"`
					Info struct {
						TotalTokenUsage struct {
							InputTokens       int64 `json:"input_tokens"`
							CachedInputTokens int64 `json:"cached_input_tokens"`
							OutputTokens      int64 `json:"output_tokens"`
							ReasoningTokens   int64 `json:"reasoning_output_tokens"`
							TotalTokens       int64 `json:"total_tokens"`
						} `json:"total_token_usage"`
						ModelContextWindow int64 `json:"model_context_window"`
					} `json:"info"`
					RateLimits struct {
						LimitID string `json:"limit_id"`
						Primary *struct {
							UsedPercent   float64 `json:"used_percent"`
							WindowMinutes int     `json:"window_minutes"`
							ResetsAt      int64   `json:"resets_at"`
						} `json:"primary"`
						Credits struct {
							HasCredits bool   `json:"has_credits"`
							Balance    string `json:"balance"`
						} `json:"credits"`
						PlanType string `json:"plan_type"`
					} `json:"rate_limits"`
				} `json:"payload"`
			}

			if err := json.Unmarshal(line, &ev); err == nil && ev.Payload.Type == "token_count" {
				info := ev.Payload.Info.TotalTokenUsage
				lastUsage.TotalTokens = info.TotalTokens
				lastUsage.InputTokens = info.InputTokens
				lastUsage.OutputTokens = info.OutputTokens
				lastUsage.CachedInputTokens = info.CachedInputTokens
				lastUsage.ReasoningTokens = info.ReasoningTokens
				lastUsage.ModelContextWindow = ev.Payload.Info.ModelContextWindow

				if ev.Payload.RateLimits.Primary != nil {
					lastUsage.RateLimitUsedPct = ev.Payload.RateLimits.Primary.UsedPercent
					if ev.Payload.RateLimits.Primary.ResetsAt > 0 {
						lastUsage.RateLimitResetAt = time.Unix(ev.Payload.RateLimits.Primary.ResetsAt, 0)
					}
					if lastUsage.RateLimitUsedPct >= 100.0 {
						lastUsage.Exhausted = true
						if lastUsage.ExhaustionReason == "" {
							lastUsage.ExhaustionReason = "usage_limit_exceeded"
						}
					}
				}
				if ev.Payload.RateLimits.PlanType != "" {
					lastUsage.PlanType = ev.Payload.RateLimits.PlanType
				}
				lastUsage.CreditsRemaining = ev.Payload.RateLimits.Credits.Balance
				found = true
			}
		}

		if strings.Contains(lineStr, "task_complete") {
			var ev struct {
				Payload struct {
					Type  string `json:"type"`
					Error *struct {
						Message        string `json:"message"`
						CodexErrorInfo string `json:"codex_error_info"`
					} `json:"error"`
				} `json:"payload"`
			}
			if err := json.Unmarshal(line, &ev); err == nil && ev.Payload.Type == "task_complete" {
				if ev.Payload.Error != nil && quotaError(ev.Payload.Error.CodexErrorInfo, ev.Payload.Error.Message, 0) {
					lastUsage.Exhausted = true
					if ev.Payload.Error.CodexErrorInfo != "" {
						lastUsage.ExhaustionReason = ev.Payload.Error.CodexErrorInfo
					} else {
						lastUsage.ExhaustionReason = ev.Payload.Error.Message
					}
					found = true
				}
			}
		}
	}
	return lastUsage, found
}

func parseAgyTranscriptTokens(usage *TokenUsage) bool {
	agyBrainTokensCacheMu.RLock()
	if time.Since(agyBrainTokensCachedAt) < 10*time.Second {
		cached := agyBrainTokensCache
		agyBrainTokensCacheMu.RUnlock()
		usage.InputTokens = cached.InputTokens
		usage.OutputTokens = cached.OutputTokens
		usage.CachedInputTokens = cached.CachedInputTokens
		usage.TotalTokens = cached.TotalTokens
		return cached.TotalTokens > 0
	}
	agyBrainTokensCacheMu.RUnlock()

	home, _ := configpaths.HomeDir()
	brainDir := filepath.Join(home, ".gemini", "antigravity-cli", "brain")
	if _, err := os.Stat(brainDir); err != nil {
		return false
	}

	var latestLog string
	var latestMod time.Time

	_ = filepath.Walk(brainDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if !info.IsDir() && filepath.Base(path) == "transcript.jsonl" {
			if info.ModTime().After(latestMod) {
				latestMod = info.ModTime()
				latestLog = path
			}
		}
		return nil
	})

	if latestLog == "" {
		return false
	}

	f, err := os.Open(latestLog)
	if err != nil {
		return false
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 10*1024*1024)

	var parsed TokenUsage
	found := false
	for scanner.Scan() {
		line := scanner.Bytes()
		if !strings.Contains(string(line), `"input_tokens"`) {
			continue
		}

		var row struct {
			InputTokens     int64 `json:"input_tokens"`
			CacheReadTokens int64 `json:"cache_read_tokens"`
			OutputTokens    int64 `json:"output_tokens"`
		}
		if err := json.Unmarshal(line, &row); err == nil && (row.InputTokens > 0 || row.OutputTokens > 0) {
			parsed.InputTokens += row.InputTokens
			parsed.OutputTokens += row.OutputTokens
			parsed.CachedInputTokens += row.CacheReadTokens
			parsed.TotalTokens = parsed.InputTokens + parsed.OutputTokens
			found = true
		}
	}

	agyBrainTokensCacheMu.Lock()
	agyBrainTokensCache = parsed
	agyBrainTokensCachedAt = time.Now()
	agyBrainTokensCacheMu.Unlock()

	usage.InputTokens = parsed.InputTokens
	usage.OutputTokens = parsed.OutputTokens
	usage.CachedInputTokens = parsed.CachedInputTokens
	usage.TotalTokens = parsed.TotalTokens
	return found
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
	scanner.Buffer(make([]byte, 65536), 10*1024*1024)
	type counts struct {
		Input  int64 `json:"input_tokens"`
		Output int64 `json:"output_tokens"`
		Cached int64 `json:"cache_read_input_tokens"`
	}
	for scanner.Scan() {
		line := scanner.Bytes()
		if reason := quotaEvent(line); reason != "" {
			usage.Exhausted = true
			usage.ExhaustionReason = reason
			found = true
		}
		var entry struct {
			Usage   counts `json:"usage"`
			Message struct {
				Usage counts `json:"usage"`
			} `json:"message"`
		}
		if json.Unmarshal(line, &entry) != nil {
			continue
		}
		c := entry.Usage
		if c.Input == 0 && c.Output == 0 {
			c = entry.Message.Usage
		}
		if c.Input != 0 || c.Output != 0 || c.Cached != 0 {
			usage.InputTokens += c.Input
			usage.OutputTokens += c.Output
			usage.CachedInputTokens += c.Cached
			usage.TotalTokens = usage.InputTokens + usage.OutputTokens
			found = true
		}
	}
	return usage, found
}

func quotaError(code, message string, status int) bool {
	if status == 429 {
		return true
	}
	switch strings.ToLower(code) {
	case "usage_limit_exceeded", "rate_limit_exceeded", "rate_limit_error", "insufficient_quota", "resource_exhausted":
		return true
	}
	lower := strings.ToLower(message)
	return strings.Contains(lower, "you've hit your usage limit") || strings.Contains(lower, "quota exceeded") || strings.Contains(lower, "rate limit exceeded")
}

// Only provider error envelopes can trigger automatic rotation; quoted user text cannot.
func quotaEvent(line []byte) string {
	type apiError struct {
		Type    string `json:"type"`
		Code    string `json:"code"`
		Info    string `json:"codex_error_info"`
		Message string `json:"message"`
		Status  int    `json:"status"`
	}
	var ev struct {
		Type    string    `json:"type"`
		Subtype string    `json:"subtype"`
		Error   *apiError `json:"error"`
		Payload struct {
			Type    string    `json:"type"`
			Error   *apiError `json:"error"`
			Message string    `json:"message"`
			Info    string    `json:"codex_error_info"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return ""
	}
	var e *apiError
	if ev.Type == "error" || (ev.Type == "system" && ev.Subtype == "api_error") {
		e = ev.Error
	}
	if ev.Type == "event_msg" && (ev.Payload.Type == "error" || ev.Payload.Type == "task_complete") {
		e = ev.Payload.Error
		if e == nil && ev.Payload.Type == "error" {
			e = &apiError{Info: ev.Payload.Info, Message: ev.Payload.Message}
		}
	}
	if e != nil && (quotaError(e.Type, e.Message, e.Status) || quotaError(e.Code, "", 0) || quotaError(e.Info, "", 0)) {
		return "provider_quota_exhausted"
	}
	return ""
}
