package deck

import (
	"crypto/md5"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

var nonAlphaNumericRegex = regexp.MustCompile(`[^a-zA-Z0-9_\-]+`)

func sanitizeSessionSlug(s string) string {
	clean := nonAlphaNumericRegex.ReplaceAllString(strings.ToLower(s), "_")
	clean = strings.Trim(clean, "_")
	if len(clean) > 20 {
		clean = clean[:20]
	}
	if clean == "" {
		clean = "task"
	}
	return clean
}

// CreateTaskOptions specifies parameters for launching a new task.
type CreateTaskOptions struct {
	Title         string
	ProjectPath   string
	GroupPath     string
	Tool          string // agy, claude, codex, opencode
	Account       string // e.g. danielmcf15@gmail.com
	SkillProfile  string // e.g. core, backend, web, minimal, all
	InitialPrompt string
}

// CreateTask provisions a new task in state.db, sets up the isolated sandbox
// with required skills and accounts, and launches a background tmux session.
func (s *StateDB) CreateTask(virt *harness.Virtualizer, opts CreateTaskOptions) (*InstanceRecord, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("state.db connection is nil")
	}

	title := strings.TrimSpace(opts.Title)
	if title == "" {
		title = fmt.Sprintf("task-%d", time.Now().Unix()%100000)
	}

	projectPath := strings.TrimSpace(opts.ProjectPath)
	if projectPath == "" {
		if cwd, err := os.Getwd(); err == nil {
			projectPath = cwd
		} else {
			projectPath, _ = os.UserHomeDir()
		}
	} else if strings.HasPrefix(projectPath, "~/") {
		home, _ := os.UserHomeDir()
		projectPath = filepath.Join(home, projectPath[2:])
	}

	groupPath := strings.Trim(opts.GroupPath, "/")
	if groupPath == "" {
		groupPath = "my-sessions"
	}

	tool, cmd, err := NormalizeHarnessName(opts.Tool)
	if err != nil {
		tool = "agy"
		cmd = "agy"
	}

	account := strings.TrimSpace(opts.Account)
	if account == "" {
		account = "default"
	}

	skillProfile := strings.TrimSpace(opts.SkillProfile)
	if skillProfile == "" {
		skillProfile = "core"
	}

	if tool == "vox-studio" {
		if groupPath == "my-sessions" {
			groupPath = "Creative/VideoStudio"
		}
		home, _ := os.UserHomeDir()
		if projectPath == "" || projectPath == home {
			slug := sanitizeSessionSlug(title)
			projectPath = filepath.Join(home, "vox-video-studio", "projects", slug)
		}
		_ = os.MkdirAll(projectPath, 0700)
		cmd = fmt.Sprintf("node /home/falluba/vox-video-studio/scripts/launch.ts new --project %s --title %q --voice --provider agy", projectPath, title)
		if !strings.Contains(skillProfile, "video") {
			skillProfile = "video," + skillProfile
		}
	}

	// 1. Ensure group exists
	if err := s.CreateGroup(groupPath); err != nil {
		return nil, fmt.Errorf("create group %s: %w", groupPath, err)
	}

	// 2. Generate unique task ID & tmux session name
	hash := md5.Sum([]byte(fmt.Sprintf("%s-%d", title, time.Now().UnixNano())))
	id := fmt.Sprintf("task-%x-%d", hash[:4], time.Now().Unix())
	slug := sanitizeSessionSlug(title)
	tmuxSession := fmt.Sprintf("agentdeck_%s_%x", slug, hash[:4])

	// 3. Resolve skills and prepare sandbox
	if virt == nil {
		virt = harness.NewVirtualizer("", "")
	}
	var skillsToProject []string
	if skillProfile == "all" {
		skillsToProject, _ = harness.ListAllSkillNames(virt.SkillsPath())
	} else if skillProfile != "" {
		cfg, pErr := harness.LoadSkillProfiles("")
		if pErr == nil {
			resolved, rErr := cfg.ResolveSkills(skillProfile, "")
			if rErr == nil {
				skillsToProject = resolved
			}
		}
	}
	if len(skillsToProject) == 0 {
		skillsToProject = []string{
			"shadcn", "verification", "auth", "react-best-practices",
			"env-vars", "deployments-cicd", "bootstrap",
		}
	}

	if tool == "agy" || tool == "gemini" {
		sb := swapbridge.NewSwapBridge("")
		token, _ := sb.ReadAccountToken(account)
		_ = virt.PrepareAgyAccount(account, token, account)
	}

	envOverrides, err := virt.PrepareSandboxWithSkills(tool, account, skillsToProject)
	if err != nil {
		return nil, fmt.Errorf("prepare sandbox: %w", err)
	}
	_ = virt.ReplicateToolPermissions(tool, account, projectPath)

	// 4. Construct tool_data JSON
	toolDataMap := map[string]interface{}{
		"skill_profile": skillProfile,
		"created_by":    "ai-desk",
		"created_at":    time.Now().Unix(),
	}
	if opts.InitialPrompt != "" {
		toolDataMap["initial_prompt"] = opts.InitialPrompt
	}
	toolDataBytes, _ := json.Marshal(toolDataMap)
	toolDataJSON := string(toolDataBytes)

	// 5. Insert instance record into SQLite
	now := time.Now().Unix()
	query := `INSERT INTO instances (
		id, title, project_path, group_path, sort_order, command, wrapper, tool,
		status, tmux_session, created_at, last_accessed, account, tool_data
	) VALUES (?, ?, ?, ?, 0, ?, '', ?, 'running', ?, ?, ?, ?, ?)`

	_, err = s.db.Exec(query,
		id, title, projectPath, groupPath, cmd, tool,
		tmuxSession, now, now, account, toolDataJSON,
	)
	if err != nil {
		return nil, fmt.Errorf("insert instance record: %w", err)
	}

	// 6. Spawn tmux session if available
	var envArgs []string
	for k, v := range envOverrides {
		envArgs = append(envArgs, fmt.Sprintf("%s=%s", k, quoteArg(v)))
	}
	launchCmd := cmd
	if len(envArgs) > 0 {
		launchCmd = fmt.Sprintf("env %s %s", strings.Join(envArgs, " "), cmd)
	}

	tmuxCmd := exec.Command("tmux", "new-session", "-d", "-s", tmuxSession, "-c", projectPath, launchCmd)
	if tErr := tmuxCmd.Run(); tErr == nil {
		for k, v := range envOverrides {
			_ = exec.Command("tmux", "set-environment", "-t", tmuxSession, k, v).Run()
		}

		if opts.InitialPrompt != "" {
			go func() {
				time.Sleep(1500 * time.Millisecond)
				_ = exec.Command("tmux", "send-keys", "-t", tmuxSession, opts.InitialPrompt, "Enter").Run()
			}()
		}
	}

	// Invalidate inspection cache
	InvalidateInspectionCache("")

	inst := &InstanceRecord{
		ID:           id,
		Title:        title,
		ProjectPath:  projectPath,
		GroupPath:    groupPath,
		SortOrder:    0,
		Command:      cmd,
		Wrapper:      "",
		Tool:         tool,
		Status:       "running",
		TmuxSession:  tmuxSession,
		CreatedAt:    now,
		LastAccessed: now,
		Account:      account,
		ToolData:     toolDataJSON,
	}

	return inst, nil
}
