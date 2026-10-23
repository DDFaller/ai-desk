package deck

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/rotation"
	"sync"
)

// SwitchOptions specifies settings when switching a running session's harness.
type SwitchOptions struct {
	TargetTool     string // agy, claude, codex, opencode
	AccountID      string // defaults to "default"
	SkillProfile   string // e.g. "core", "web", "minimal", "all"
	IncludeHandoff bool   // defaults to true
	CustomPrompt   string // extra directive passed to target agent
	PreserveSkills bool   // keep active skills from previous harness
	CreateNewTask  bool   // if true, forks into a brand new task instead of mutating in-place
}

// SwitchResult contains metadata about the performed harness switch.
type SwitchResult struct {
	Instance       *InstanceRecord
	OldTool        string
	NewTool        string
	HandoffPath    string
	HandoffSummary string
	TmuxSession    string
	LaunchCommand  string
	SandboxPath    string
	SkillsAttached int
}

// NormalizeHarnessName validates and normalizes an AI harness name.
func NormalizeHarnessName(name string) (tool string, command string, err error) {
	lower := strings.ToLower(strings.TrimSpace(name))
	switch lower {
	case "agy", "gemini", "antigravity":
		return "agy", "agy", nil
	case "claude", "claude-code":
		return "claude", "claude", nil
	case "codex", "codex-cli":
		return "codex", "codex", nil
	case "opencode":
		return "opencode", "opencode", nil
	case "vox-studio", "vox", "vox-video-studio":
		return "vox-studio", "vox-studio", nil
	default:
		return "", "", fmt.Errorf("unsupported target harness %q (supported: agy, claude, codex, opencode, vox-studio)", name)
	}
}

// SwitchHarness dynamically migrates a running session from its current harness to a new one.
func SwitchHarness(sdb *StateDB, virt *harness.Virtualizer, sessionIDOrTitle string, opts SwitchOptions) (*SwitchResult, error) {
	if sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}
	if virt == nil {
		virt = harness.NewVirtualizer("", "")
	}

	// 1. Locate instance
	initial, err := sdb.FindInstance(sessionIDOrTitle)
	if err != nil {
		return nil, err
	}
	unlock, err := sdb.lockTransition(initial.ID)
	if err != nil {
		return nil, err
	}
	defer unlock()
	inst, err := sdb.FindInstance(sessionIDOrTitle)
	if err != nil {
		return nil, err
	}

	targetTool, targetCmd, err := NormalizeHarnessName(opts.TargetTool)
	if err != nil {
		return nil, err
	}

	sessionCfg, err := ReadSessionConfig(inst)
	if err != nil {
		return nil, err
	}
	oldTool := inst.Tool
	accountID := opts.AccountID
	if accountID == "" || accountID == "default" {
		accountID = inst.Account
	}
	if targetTool != oldTool && opts.AccountID == "" {
		accountID = "default"
	}
	accountID, err = RouteAccount(targetTool, sessionCfg.AccountProfile, accountID)
	if err != nil {
		return nil, err
	}

	result := &SwitchResult{
		Instance:    inst,
		OldTool:     oldTool,
		NewTool:     targetTool,
		TmuxSession: inst.TmuxSession,
		SandboxPath: virt.SandboxPath(targetTool, accountID),
	}

	// 2. Synthesize context handoff brief if requested
	var handoffPath string
	var handoffContent string
	if opts.IncludeHandoff {
		var dialogueContext string
		if cleanTurns := ExtractStructuredConversation(inst); cleanTurns != "" {
			dialogueContext = cleanTurns
		} else if inst.TmuxSession != "" {
			tail, _ := CaptureTmuxBuffer(inst.TmuxSession, 120)
			dialogueContext = SanitizeTerminalCapture(tail)
		}

		branch, status, diffStat, _ := CaptureGitDelta(inst.ProjectPath)

		brief := &HandoffBrief{
			SessionID:    inst.ID,
			Title:        inst.Title,
			ProjectPath:  inst.ProjectPath,
			SourceTool:   oldTool,
			TargetTool:   targetTool,
			GitBranch:    branch,
			GitStatus:    status,
			GitDiffStat:  diffStat,
			TerminalTail: dialogueContext,
			CustomPrompt: opts.CustomPrompt,
			Timestamp:    time.Now(),
		}

		handoffContent = BuildHandoffBrief(brief)
		savedPath, saveErr := SaveHandoffBrief(inst.ProjectPath, inst.ID, handoffContent)
		if saveErr == nil {
			handoffPath = savedPath
			result.HandoffPath = savedPath
			result.HandoffSummary = fmt.Sprintf("Captured %d lines context, git branch %q (%d status items)",
				len(strings.Split(dialogueContext, "\n")), branch, len(strings.Split(status, "\n")))
		}
	}

	// 2b. If requested, fork into a brand new task instance rather than mutating in-place
	if opts.CreateNewTask {
		newTaskTitle := fmt.Sprintf("%s-%s", inst.Title, targetTool)
		if existing, _ := sdb.FindInstance(newTaskTitle); existing != nil {
			newTaskTitle = fmt.Sprintf("%s-%s-%x", inst.Title, targetTool, time.Now().UnixNano()&0xffff)
		}
		directive := ""
		if opts.IncludeHandoff && handoffPath != "" {
			directive = BuildHandoffDirective(oldTool, targetTool, handoffPath, opts.CustomPrompt)
		} else if opts.CustomPrompt != "" {
			directive = opts.CustomPrompt
		}
		profile := opts.SkillProfile
		if profile == "" {
			profile = sessionCfg.SkillProfile
			if profile == "" {
				profile = "none"
			}
		}
		createOpts := CreateTaskOptions{
			Title:          newTaskTitle,
			ProjectPath:    inst.ProjectPath,
			GroupPath:      inst.GroupPath,
			Tool:           targetTool,
			Account:        accountID,
			AccountProfile: sessionCfg.AccountProfile,
			SkillProfile:   profile,
			InitialPrompt:  directive,
		}
		if opts.PreserveSkills {
			createOpts.SelectedSkills, err = SessionSkills(virt, inst)
			if err != nil {
				return nil, err
			}
			if createOpts.SelectedSkills == nil {
				createOpts.SelectedSkills = []string{}
			}
		}
		newInst, createErr := sdb.CreateTask(virt, createOpts)
		if createErr != nil {
			return nil, fmt.Errorf("create new switched task: %w", createErr)
		}
		result.Instance = newInst
		result.NewTool = targetTool
		result.TmuxSession = newInst.TmuxSession
		result.LaunchCommand = newInst.Command
		return result, nil
	}

	// Preserve the exact selected set unless an explicit replacement was requested.
	skillsToProject, err := SessionSkills(virt, inst)
	if err != nil {
		return nil, err
	}
	if opts.SkillProfile != "" && !opts.PreserveSkills {
		cfg, err := harness.LoadSkillProfiles("")
		if err != nil {
			return nil, err
		}
		skillsToProject, err = cfg.ResolveSkills(opts.SkillProfile, virt.SkillsPath())
		if err != nil {
			return nil, err
		}
		sessionCfg.SkillProfile = opts.SkillProfile
	}
	virt = virt.ForSession(inst.ID)
	if err := virt.PrepareCredentials(targetTool, accountID); err != nil {
		return nil, err
	}

	result.SandboxPath = virt.SandboxPath(targetTool, accountID)
	envOverrides, err := virt.PrepareSandboxWithSkills(targetTool, accountID, skillsToProject)
	if err != nil {
		return nil, fmt.Errorf("prepare sandbox for %s: %w", targetTool, err)
	}
	_ = virt.ReplicateToolPermissions(targetTool, accountID, inst.ProjectPath)
	result.SkillsAttached = len(skillsToProject)

	if handoffContent != "" && result.SandboxPath != "" {
		_ = os.WriteFile(filepath.Join(result.SandboxPath, ".ai-desk-handoff.md"), []byte(handoffContent), 0600)
	}

	// Prepare a replacement session before stopping the old process.
	oldTmux := inst.TmuxSession
	replacement := fmt.Sprintf("agentdesk_%s_%x", sanitizeSessionSlug(inst.Title), time.Now().UnixNano())
	inst.TmuxSession = replacement
	result.TmuxSession = replacement

	// Build command to launch target harness with handoff prompt
	cmdWithArgs := targetCmd
	if opts.IncludeHandoff && handoffPath != "" {
		directive := BuildHandoffDirective(oldTool, targetTool, handoffPath, opts.CustomPrompt)
		cmdWithArgs = BuildHarnessLaunchCommand(targetCmd, directive)
	} else if opts.CustomPrompt != "" {
		cmdWithArgs = BuildHarnessLaunchCommand(targetCmd, opts.CustomPrompt)
	}

	targetLaunch := cmdWithArgs
	if inst.Wrapper != "" {
		if strings.Contains(inst.Wrapper, "{command}") {
			targetLaunch = strings.ReplaceAll(inst.Wrapper, "{command}", targetLaunch)
		} else {
			targetLaunch = inst.Wrapper + " " + targetLaunch
		}
	}

	// Ensure working dir and explicit environment
	var envArgs []string
	for k, v := range envOverrides {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", k, quoteArg(v)))
	}
	cmdWithEnv := targetLaunch
	if len(envArgs) > 0 {
		cmdWithEnv = fmt.Sprintf("env %s %s", strings.Join(envArgs, " "), targetLaunch)
	}

	launchCmd := cmdWithEnv
	if inst.ProjectPath != "" {
		launchCmd = fmt.Sprintf("cd %s && %s", quoteArg(inst.ProjectPath), cmdWithEnv)
	}

	result.LaunchCommand = launchCmd

	// 5. Recreate fresh tmux session
	tmuxCmd := exec.Command("tmux", "new-session", "-d", "-s", inst.TmuxSession, "-c", inst.ProjectPath, launchCmd)
	if out, err := tmuxCmd.CombinedOutput(); err != nil {
		return nil, fmt.Errorf("launch replacement: %w: %s", err, strings.TrimSpace(string(out)))
	}
	if err := exec.Command("tmux", "has-session", "-t", "="+replacement).Run(); err != nil {
		return nil, fmt.Errorf("replacement exited during launch: %w", err)
	}
	if oldTmux != "" {
		if err := exec.Command("tmux", "kill-session", "-t", "="+oldTmux).Run(); err != nil {
			_ = exec.Command("tmux", "kill-session", "-t", "="+replacement).Run()
			return nil, fmt.Errorf("stop old session: %w", err)
		}
	}

	for k, v := range envOverrides {
		_ = exec.Command("tmux", "set-environment", "-t", inst.TmuxSession, k, v).Run()
	}

	// 6. Clean previous session IDs & persist changes in state.db
	toolDataMap := make(map[string]interface{})
	if inst.ToolData != "" {
		_ = json.Unmarshal([]byte(inst.ToolData), &toolDataMap)
	}

	// Remove stale session IDs so recovery/attach never resumes the old session
	delete(toolDataMap, "codex_session_id")
	delete(toolDataMap, "codex_detected_at")
	delete(toolDataMap, "claude_session_id")
	delete(toolDataMap, "claude_detected_at")
	delete(toolDataMap, "opencode_session_id")
	delete(toolDataMap, "gemini_session_id")
	delete(toolDataMap, "agy_session_id")
	toolDataMap["is_fresh_harness"] = true
	toolDataMap["switched_from"] = oldTool
	toolDataMap["switched_to"] = targetTool

	// Append migration record to history
	type migrationRecord struct {
		From        string `json:"from"`
		To          string `json:"to"`
		Timestamp   int64  `json:"timestamp"`
		HandoffPath string `json:"handoff_path,omitempty"`
	}

	var history []migrationRecord
	if rawHist, ok := toolDataMap["cross_harness_history"]; ok {
		rawBytes, _ := json.Marshal(rawHist)
		_ = json.Unmarshal(rawBytes, &history)
	}
	history = append(history, migrationRecord{
		From:        oldTool,
		To:          targetTool,
		Timestamp:   time.Now().Unix(),
		HandoffPath: handoffPath,
	})
	toolDataMap["cross_harness_history"] = history
	toolDataMap["last_switched_at"] = time.Now().Unix()
	toolDataMap["config_version"] = 1
	toolDataMap["account_profile"] = sessionCfg.AccountProfile
	toolDataMap["skill_profile"] = sessionCfg.SkillProfile
	toolDataMap["selected_skills"] = append([]string{}, skillsToProject...)
	toolDataMap["skills_pending"] = false

	updatedDataJSON, _ := json.Marshal(toolDataMap)
	updateQuery := `UPDATE instances SET tool = ?, command = ?, tmux_session = ?, tool_data = ?, status = 'running', last_accessed = ? WHERE id = ?`
	if _, err := sdb.db.Exec(updateQuery, targetTool, targetCmd, inst.TmuxSession, string(updatedDataJSON), time.Now().Unix(), inst.ID); err != nil {
		return nil, fmt.Errorf("update database harness: %w", err)
	}

	if accountID != inst.Account && accountID != "" {
		_ = sdb.UpdateInstanceAccount(inst.ID, accountID)
		inst.Account = accountID
	}

	// Refresh instance
	inst.Tool = targetTool
	inst.Command = targetCmd
	inst.ToolData = string(updatedDataJSON)
	result.Instance = inst

	InvalidateInspectionCache(inst.ID)
	InvalidateInspectionCache(inst.Title)
	InvalidateInspectionCache("")

	return result, nil
}

// SwitchAccount dynamically migrates an active session's account, provisions the isolated sandbox,
// injects the updated environment into the tmux session, and optionally respawns the pane.
func SwitchAccount(sdb *StateDB, virt *harness.Virtualizer, sessionIDOrTitle string, targetAccount string, respawn bool) (*InstanceRecord, error) {
	if sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}
	if virt == nil {
		virt = harness.NewVirtualizer("", "")
	}
	if targetAccount == "" {
		return nil, fmt.Errorf("target account cannot be empty")
	}

	inst, err := sdb.FindInstance(sessionIDOrTitle)
	if err != nil {
		return nil, err
	}

	tool := strings.ToLower(inst.Tool)
	if tool == "" {
		tool = "agy"
	}

	cfg, err := ReadSessionConfig(inst)
	if err != nil {
		return nil, err
	}
	targetAccount, err = RouteAccount(tool, cfg.AccountProfile, targetAccount)
	if err != nil {
		return nil, err
	}
	if respawn {
		result, err := SwitchHarness(sdb, virt, inst.ID, SwitchOptions{TargetTool: tool, AccountID: targetAccount, PreserveSkills: true, IncludeHandoff: true})
		if err != nil {
			return nil, err
		}
		return result.Instance, nil
	}
	activeSkills, err := SessionSkills(virt, inst)
	if err != nil {
		return nil, err
	}
	virt = virt.ForSession(inst.ID)
	if err := virt.PrepareCredentials(tool, targetAccount); err != nil {
		return nil, err
	}
	envOverrides, err := virt.PrepareSandboxWithSkills(tool, targetAccount, activeSkills)
	if err != nil {
		return nil, fmt.Errorf("prepare sandbox: %w", err)
	}
	if err := virt.ReplicateToolPermissions(tool, targetAccount, inst.ProjectPath); err != nil {
		return nil, err
	}

	// 3. Update tmux environment variables
	if inst.TmuxSession != "" {
		hasSess := exec.Command("tmux", "has-session", "-t", inst.TmuxSession).Run() == nil
		if hasSess {
			for k, v := range envOverrides {
				_ = exec.Command("tmux", "set-environment", "-t", inst.TmuxSession, k, v).Run()
			}

			if respawn {
				var envArgs []string
				for k, v := range envOverrides {
					envArgs = append(envArgs, fmt.Sprintf("%s=%s", k, quoteArg(v)))
				}
				cmdWithEnv := inst.Command
				if cmdWithEnv == "" {
					cmdWithEnv = fmt.Sprintf("exec %s", tool)
				}
				if len(envArgs) > 0 {
					cmdWithEnv = fmt.Sprintf("env %s %s", strings.Join(envArgs, " "), cmdWithEnv)
				}
				launchCmd := cmdWithEnv
				if inst.ProjectPath != "" {
					launchCmd = fmt.Sprintf("cd %s && %s", quoteArg(inst.ProjectPath), cmdWithEnv)
				}
				respawnCmd := exec.Command("tmux", "respawn-pane", "-k", "-t", inst.TmuxSession, launchCmd)
				if err := respawnCmd.Run(); err != nil {
					return nil, fmt.Errorf("restart failed: %w", err)
				}
			} else {
				return nil, fmt.Errorf("account changes require restart; rerun with respawn enabled")
			}
		}
	}

	// 4. Update SQLite state.db
	cfg.Version = 1
	cfg.SelectedSkills = append([]string{}, activeSkills...)
	cfg.SkillsPending = false
	if err := sdb.SaveSessionConfig(inst, cfg); err != nil {
		return nil, err
	}
	if err := sdb.UpdateInstanceAccount(inst.ID, targetAccount); err != nil {
		return nil, fmt.Errorf("update instance account: %w", err)
	}

	InvalidateInspectionCache(inst.ID)
	InvalidateInspectionCache(inst.Title)

	return sdb.FindInstance(inst.ID)
}

func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t\n$\"'\\;|&()<>`*?[]{}!#~") {
		return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
	}
	return s
}

// BuildHandoffDirective constructs a directive instructing the agent to absorb context and continue.
func BuildHandoffDirective(oldTool, targetTool, handoffPath, customPrompt string) string {
	targetFile := ".ai-desk-handoff.md"
	if handoffPath != "" {
		targetFile = handoffPath
	}
	msg := fmt.Sprintf("Acknowledge handoff from prior %s session. Review %s for recent conversation tail, git modifications, and context, then continue the objective.", oldTool, targetFile)
	if customPrompt != "" {
		msg += fmt.Sprintf(" Additional operator directive: %s", customPrompt)
	}
	return msg
}

// BuildHarnessLaunchCommand constructs the target CLI command line including the initial interactive prompt.
func BuildHarnessLaunchCommand(targetCmd, prompt string) string {
	if prompt == "" {
		return targetCmd
	}
	switch strings.ToLower(targetCmd) {
	case "agy", "gemini":
		return fmt.Sprintf("%s -i %s", targetCmd, quoteArg(prompt))
	case "claude":
		return fmt.Sprintf("%s %s", targetCmd, quoteArg(prompt))
	case "codex":
		return fmt.Sprintf("%s %s", targetCmd, quoteArg(prompt))
	case "opencode":
		return fmt.Sprintf("%s --prompt %s", targetCmd, quoteArg(prompt))
	default:
		return fmt.Sprintf("%s %s", targetCmd, quoteArg(prompt))
	}
}

// AutoFailoverSession automatically detects quota exhaustion or 429 rate limits,
// selects the next healthy account according to the active profile policy,
// and migrates the session seamlessly without manual user intervention.
var failoverMu sync.Mutex

func AutoFailoverSession(sdb *StateDB, virt *harness.Virtualizer, sessionIDOrTitle string, reason string) (*InstanceRecord, error) {
	failoverMu.Lock()
	defer failoverMu.Unlock()
	if sdb == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}
	if virt == nil {
		virt = harness.NewVirtualizer("", "")
	}

	inst, err := sdb.FindInstance(sessionIDOrTitle)
	if err != nil {
		return nil, err
	}

	var eventData struct {
		LastFailover int64 `json:"last_failover_at"`
	}
	if err := json.Unmarshal([]byte(inst.ToolData), &eventData); err == nil && time.Now().Unix()-eventData.LastFailover < 20 {
		return nil, fmt.Errorf("session just switched accounts; waiting for fresh quota telemetry")
	}
	cfg, err := rotation.LoadAccountProfiles("")
	if err != nil {
		return nil, err
	}
	sessionCfg, err := ReadSessionConfig(inst)
	if err != nil {
		return nil, err
	}
	activeProfName := sessionCfg.AccountProfile
	if activeProfName == "" {
		activeProfName = cfg.ActiveProfile
		sessionCfg.AccountProfile = activeProfName
		if err := sdb.SaveSessionConfig(inst, sessionCfg); err != nil {
			return nil, err
		}
	}
	prof, ok := cfg.Profiles[activeProfName]
	if !ok {
		return nil, fmt.Errorf("unknown account profile %q", activeProfName)
	}

	if !prof.AutoFailover {
		return nil, fmt.Errorf("auto-failover is disabled in profile %q", activeProfName)
	}

	allAccounts, err := harness.ListAccounts(inst.Tool)
	if err != nil || len(allAccounts) == 0 {
		return nil, fmt.Errorf("no accounts available in vault for failover: %v", err)
	}

	candidates := cfg.FilterAccounts(activeProfName, allAccounts)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no eligible accounts in profile %q", activeProfName)
	}

	failedID := inst.Account
	if failedID == "" || failedID == "default" {
		return nil, fmt.Errorf("session account identity is unknown")
	}

	if reason == "" {
		reason = "HTTP 429 rate limit or quota depleted"
	}

	nextAcc, err := rotation.FailoverPersisted(candidates, failedID, 20*time.Minute, prof.Policy, inst.Tool)
	if err != nil {
		return nil, fmt.Errorf("select next account on failover: %w", err)
	}

	if strings.EqualFold(nextAcc.Email, inst.Account) || strings.EqualFold(nextAcc.ID, inst.Account) {
		return nil, fmt.Errorf("no alternate account available in profile %q for failover", activeProfName)
	}

	targetEmail := nextAcc.Email
	if targetEmail == "" {
		targetEmail = nextAcc.ID
	}

	// Perform account switch and respawn pane
	updatedInst, err := SwitchAccount(sdb, virt, inst.ID, targetEmail, true)
	if err != nil {
		return nil, fmt.Errorf("switch account during failover: %w", err)
	}

	// Update tool data with failover record
	toolDataMap := make(map[string]interface{})
	if updatedInst.ToolData != "" {
		_ = json.Unmarshal([]byte(updatedInst.ToolData), &toolDataMap)
	}
	type failoverRecord struct {
		FromAccount string `json:"from_account"`
		ToAccount   string `json:"to_account"`
		Reason      string `json:"reason"`
		Timestamp   int64  `json:"timestamp"`
		Profile     string `json:"profile"`
	}
	var failovers []failoverRecord
	if raw, ok := toolDataMap["auto_failovers"]; ok {
		rawBytes, _ := json.Marshal(raw)
		_ = json.Unmarshal(rawBytes, &failovers)
	}
	failovers = append(failovers, failoverRecord{
		FromAccount: failedID,
		ToAccount:   targetEmail,
		Reason:      reason,
		Timestamp:   time.Now().Unix(),
		Profile:     activeProfName,
	})
	toolDataMap["auto_failovers"] = failovers
	toolDataMap["last_failover_at"] = time.Now().Unix()
	newToolData, _ := json.Marshal(toolDataMap)

	if err := sdb.UpdateInstanceToolData(updatedInst.ID, string(newToolData)); err != nil {
		return updatedInst, err
	}
	updatedInst.ToolData = string(newToolData)

	return updatedInst, nil
}
