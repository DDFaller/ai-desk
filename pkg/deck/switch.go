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
	"github.com/falluba/ai-desk/pkg/swapbridge"
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
	inst, err := sdb.FindInstance(sessionIDOrTitle)
	if err != nil {
		return nil, err
	}

	targetTool, targetCmd, err := NormalizeHarnessName(opts.TargetTool)
	if err != nil {
		return nil, err
	}

	oldTool := inst.Tool
	accountID := opts.AccountID
	if accountID == "" || accountID == "default" {
		accountID = inst.Account
	}
	if accountID == "" || accountID == "default" {
		sb := swapbridge.NewSwapBridge("")
		if accs, err := sb.LoadAccounts(); err == nil && len(accs) > 0 {
			accountID = accs[0].Email
		}
	}
	if accountID == "" {
		accountID = "default"
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
			profile = "core"
		}
		createOpts := CreateTaskOptions{
			Title:         newTaskTitle,
			ProjectPath:   inst.ProjectPath,
			GroupPath:     inst.GroupPath,
			Tool:          targetTool,
			Account:       accountID,
			SkillProfile:  profile,
			InitialPrompt: directive,
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

	// 3. Resolve and project skills for the new harness
	var skillsToProject []string
	if opts.PreserveSkills {
		// Inherit currently attached skills
		active, _ := virt.ListAttachedSkills(oldTool, accountID)
		skillsToProject = active
	} else if opts.SkillProfile != "" {
		cfg, pErr := harness.LoadSkillProfiles("")
		if pErr == nil {
			resolved, rErr := cfg.ResolveSkills(opts.SkillProfile, "")
			if rErr == nil {
				skillsToProject = resolved
			}
		}
	} else {
		// Default to core profile
		skillsToProject = []string{
			"shadcn", "verification", "auth", "react-best-practices",
			"env-vars", "deployments-cicd", "bootstrap",
		}
	}

	// Ensure isolated account credentials exist for agy
	if targetTool == "agy" || targetTool == "gemini" {
		sb := swapbridge.NewSwapBridge("")
		token, err := sb.ReadAccountToken(accountID)
		if err != nil || token == "" {
			if accs, err := sb.LoadAccounts(); err == nil && len(accs) > 0 {
				token, _ = sb.ReadAccountToken(accs[0].Email)
				accountID = accs[0].Email
			}
		}
		_ = virt.PrepareAgyAccount(accountID, token, accountID)
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

	// 4. Terminate lingering old tmux session and prepare launch command
	if inst.TmuxSession != "" {
		_ = exec.Command("tmux", "kill-session", "-t", inst.TmuxSession).Run()
	} else {
		slug := sanitizeSessionSlug(inst.Title)
		inst.TmuxSession = fmt.Sprintf("agentdeck_%s_%x", slug, time.Now().UnixNano()&0xffff)
	}
	result.TmuxSession = inst.TmuxSession

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
	if err := tmuxCmd.Run(); err != nil {
		_ = exec.Command("tmux", "new-session", "-d", "-s", inst.TmuxSession, launchCmd).Run()
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

	// 1. Prepare isolated account credentials in sandbox
	if tool == "agy" || tool == "gemini" {
		sb := swapbridge.NewSwapBridge("")
		token, _ := sb.ReadAccountToken(targetAccount)
		if err := virt.PrepareAgyAccount(targetAccount, token, targetAccount); err != nil {
			return nil, fmt.Errorf("prepare agy account: %w", err)
		}
	}

	// 2. Prepare sandbox environment and project existing skills
	activeSkills, _ := virt.ListAttachedSkills(tool, inst.Account)
	if len(activeSkills) == 0 {
		activeSkills, _ = virt.ListAttachedSkills(tool, "default")
	}
	envOverrides, err := virt.PrepareSandboxWithSkills(tool, targetAccount, activeSkills)
	if err != nil {
		return nil, fmt.Errorf("prepare sandbox for %s: %w", targetAccount, err)
	}
	_ = virt.ReplicateToolPermissions(tool, targetAccount, inst.ProjectPath)

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
				_ = respawnCmd.Run()
			} else {
				notifyText := fmt.Sprintf("# Note: Session account switched to %s\n", targetAccount)
				_ = exec.Command("tmux", "send-keys", "-t", inst.TmuxSession, notifyText).Run()
			}
		}
	}

	// 4. Update SQLite state.db
	if err := sdb.UpdateInstanceAccount(inst.ID, targetAccount); err != nil {
		return nil, fmt.Errorf("update instance account: %w", err)
	}

	InvalidateInspectionCache(inst.ID)
	InvalidateInspectionCache(inst.Title)

	return sdb.FindInstance(inst.ID)
}

func quoteArg(s string) string {
	if strings.ContainsAny(s, " \t\n$\"'\\") {
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
func AutoFailoverSession(sdb *StateDB, virt *harness.Virtualizer, sessionIDOrTitle string, reason string) (*InstanceRecord, error) {
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

	cfg, err := rotation.LoadAccountProfiles("")
	if err != nil {
		cfg = rotation.DefaultAccountProfilesConfig()
	}

	activeProfName := cfg.ActiveProfile
	prof, ok := cfg.Profiles[activeProfName]
	if !ok {
		prof = rotation.AccountProfile{
			Name:         "default",
			Policy:       rotation.PolicyBalanced,
			AutoFailover: true,
		}
	}

	if !prof.AutoFailover {
		return nil, fmt.Errorf("auto-failover is disabled in profile %q", activeProfName)
	}

	sb := swapbridge.NewSwapBridge("")
	allAccounts, err := sb.LoadAccounts()
	if err != nil || len(allAccounts) == 0 {
		return nil, fmt.Errorf("no accounts available in vault for failover: %v", err)
	}

	candidates := cfg.FilterAccounts(activeProfName, allAccounts)
	if len(candidates) <= 1 {
		// If only 1 or 0 accounts in profile, fallback to all accounts
		candidates = allAccounts
	}

	engine := rotation.NewEngine(nil)
	failedID := inst.Account
	if failedID == "" {
		failedID = candidates[0].Email
	}

	if reason == "" {
		reason = "HTTP 429 rate limit or quota depleted"
	}

	nextAcc, err := engine.Failover(candidates, failedID, 20*time.Minute, prof.Policy)
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

	_ = sdb.UpdateInstanceToolData(updatedInst.ID, string(newToolData))
	updatedInst.ToolData = string(newToolData)

	return updatedInst, nil
}
