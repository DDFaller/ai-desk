package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/falluba/ai-desk/pkg/assistant"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/routines"
	"github.com/falluba/ai-desk/pkg/rtk"
	"github.com/falluba/ai-desk/pkg/swapbridge"
	"github.com/falluba/ai-desk/pkg/voice"
	"github.com/mattn/go-runewidth"
)

const Version = "0.3.5"

type FocusPane int

const (
	FocusSelection FocusPane = iota
	FocusTask
	FocusStatus
)

type Model struct {
	focus            FocusPane
	sessions         []SessionItem
	selectedSession  int
	accounts         []rotation.AccountStatus
	selectedAccount  int
	providerList     []providers.ProviderConfig
	selectedProvider int
	activeAccount    string

	// Hierarchical Groups & Tree (Pane 1: Selection)
	sdb              *deck.StateDB
	treeRoots        []*deck.GroupNode
	treeItems        []deck.TreeItem
	selectedTreeIdx  int
	treeScrollOffset int

	// The Task (Pane 2: Live Terminal Capture)
	taskScrollOffset int

	// Per-Session Status (Pane 3: Live Telemetry)
	currentInspection *deck.SessionInspection

	// Full-screen Modals
	showInspector         bool
	showSkillsOverlay     bool
	skillsConfig          *harness.SkillProfilesConfig
	selectedProfile       int
	profileKeys           []string
	skillCatalog          bool
	skillQuery            string
	skillPreview          string
	newTaskAccountProfile string

	// Cascade Delete Dialog
	showDeleteDialog bool
	deletePrompt     string
	deleteIsGroup    bool
	deleteTarget     string

	// Switch Harness Modal
	showSwitchDialog     bool
	switchTargetInst     *deck.InstanceRecord
	switchTargetToolIdx  int
	switchWithHandoff    bool
	switchPreserveSkills bool
	switchCreateNewTask  bool

	// Switch Account Modal
	showAccountDialog   bool
	accountTargetInst   *deck.InstanceRecord
	accountModalIdx     int
	accountModalRespawn bool

	// New Task Modal
	showNewTaskDialog      bool
	newTaskFieldFocus      int // 0: Title, 1: Path, 2: Group, 3: Harness, 4: Account, 5: Skill, 6: Prompt
	newTaskTitle           string
	newTaskPath            string
	newTaskGroup           string
	newTaskToolIdx         int
	newTaskAccountIdx      int
	newTaskSkillCursor     int
	newTaskSkillProfiles   map[string]bool
	newTaskPrompt          string
	newTaskTitleInput      TextInputState
	newTaskPathInput       TextInputState
	newTaskGroupInput      TextInputState
	newTaskPromptInput     TextInputState
	newTaskPathCycler      CompletionCycler
	newTaskGroupCycler     CompletionCycler
	newTaskRecentPaths     []string
	newTaskRecentIdx       int
	newTaskAutoDeriveTitle bool
	newTaskKnownGroups     []string

	// Command Palette Modal ([?] or Ctrl+P)
	showCommandPalette bool
	commandPaletteIdx  int

	// Routines Panel Modal ([r])
	showRoutinesDialog  bool
	routinesList        []routines.RoutineRecord
	routinesSelectedIdx int

	// Voice & Virtual Assistant Panel Modal ([v])
	showVoiceDialog    bool
	voiceModalIdx      int
	voiceStatusCache   *voice.VoiceStatus
	statusScrollOffset int

	width  int
	height int

	// Mouse interaction tracking
	lastClickTime time.Time
	lastClickIdx  int
	lastClickID   string

	toast       string
	toastExpiry time.Time
	err         error

	sb  *swapbridge.SwapBridge
	v   *harness.Virtualizer
	reg *providers.Registry
}

type HarnessOption struct {
	ID   string
	Name string
	Desc string
}

var availableHarnesses = []HarnessOption{
	{"agy", "Google Antigravity (agy)", "Skills, subagents, and Claude/Gemini model switching"},
	{"claude", "Anthropic Claude Code (claude)", "Agentic codebase engineering & tool calling"},
	{"codex", "OpenAI Codex CLI (codex)", "Deterministic repo work and code refactoring"},
	{"opencode", "OpenCode CLI (opencode)", "Multi-provider custom endpoint harness"},
	{"vox-studio", "Vox Video Studio (vox)", "Local voice & Lavish video workspace"},
}

var availableSkillProfiles = []string{
	"core",
	"web",
	"backend",
	"video",
	"minimal",
	"all",
	"none",
}

type CommandPaletteItem struct {
	Key         string
	Title       string
	Description string
	ActionID    string
}

var commandPaletteItems = []CommandPaletteItem{
	{Key: "Enter", Title: "Attach to Tmux Session", Description: "Connect interactively to the selected agent terminal", ActionID: "attach"},
	{Key: "n", Title: "Create New Task", Description: "Launch a new isolated agent session with chosen harness and skills", ActionID: "new_task"},
	{Key: "r", Title: "Routines & Background Cron", Description: "View and trigger scheduled recurring agent tasks", ActionID: "routines"},
	{Key: "v", Title: "Voice & Virtual Assistant", Description: "Resident ASR/TTS voice copilot & Grok-bot autonomous assistant", ActionID: "voice"},
	{Key: "w", Title: "Switch AI Harness", Description: "Migrate running task across agy, claude, codex, or opencode", ActionID: "switch_harness"},
	{Key: "a", Title: "Switch Account", Description: "Reassign session to another Google/Claude/Codex profile", ActionID: "switch_account"},
	{Key: "i", Title: "Inspect Session Telemetry", Description: "Deep diagnostics, prompt tax analysis, token usage, and logs", ActionID: "inspect"},
	{Key: "s", Title: "Skills Manager Overlay", Description: "Select and hot-reload conscious agent skill profiles", ActionID: "skills"},
	{Key: "S", Title: "SaaS Tech Stack Detector", Description: "Reverse engineer any SaaS tech stack in under 10 seconds", ActionID: "tech_stack"},
	{Key: "f", Title: "Auto-Failover Quota", Description: "Trigger immediate quota failover to the next healthy account", ActionID: "failover"},
	{Key: "d", Title: "Cascade Delete", Description: "Terminate and delete current task session or entire group", ActionID: "delete"},
	{Key: "R", Title: "Refresh All State", Description: "Reload SQLite database, group tree, accounts, and telemetry", ActionID: "refresh"},
	{Key: "q", Title: "Quit AI Desk", Description: "Exit AI-Desk command center (background tmux sessions keep running)", ActionID: "quit"},
}

func NewModel() Model {
	sb := swapbridge.NewSwapBridge("")
	v := harness.NewVirtualizer("", "")
	reg, _ := providers.Load("")
	skillsCfg, skillsErr := harness.LoadSkillProfiles("")
	if skillsErr != nil {
		skillsCfg = &harness.SkillProfilesConfig{Profiles: map[string]harness.ProfileDefinition{"none": {}}}
	}

	providerSlice := make([]providers.ProviderConfig, 0, len(reg.Providers))
	for _, p := range reg.Providers {
		providerSlice = append(providerSlice, p)
	}

	pKeys := make([]string, 0, len(skillsCfg.Profiles))
	for k := range skillsCfg.Profiles {
		pKeys = append(pKeys, k)
	}

	m := Model{
		focus:        FocusSelection,
		sb:           sb,
		v:            v,
		reg:          reg,
		skillsConfig: skillsCfg,
		providerList: providerSlice,
		profileKeys:  pKeys,
		lastClickIdx: -1,
	}
	m.reload()
	if len(m.accounts) > 0 {
		m.activeAccount = m.accounts[0].Email
	}
	return m
}

func (m *Model) reload() {
	deck.InvalidateInspectionCache("")
	m.sessions = FetchTmuxSessions()
	accounts, _ := m.sb.LoadAccounts()
	m.accounts = accounts

	// Load hierarchical tree from state.db
	if sdb, err := deck.OpenStateDB(""); err == nil {
		m.sdb = sdb
		groups, _ := sdb.LoadGroups()
		instances, _ := sdb.LoadInstances()
		m.treeRoots = deck.BuildGroupTree(groups, instances)
		m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
	}

	if m.selectedTreeIdx >= len(m.treeItems) && len(m.treeItems) > 0 {
		m.selectedTreeIdx = len(m.treeItems) - 1
	}
	if m.selectedSession >= len(m.sessions) && len(m.sessions) > 0 {
		m.selectedSession = len(m.sessions) - 1
	}
	if m.selectedAccount >= len(m.accounts) && len(m.accounts) > 0 {
		m.selectedAccount = len(m.accounts) - 1
	}

	_ = m.updateHoveredInspection()
}

type sessionInspectionLoadedMsg struct {
	sessionID  string
	inspection *deck.SessionInspection
}

func (m *Model) updateHoveredInspection() tea.Cmd {
	if len(m.treeItems) == 0 || m.selectedTreeIdx < 0 || m.selectedTreeIdx >= len(m.treeItems) {
		m.currentInspection = nil
		return nil
	}
	item := m.treeItems[m.selectedTreeIdx]
	if item.IsGroup && item.GroupNode != nil {
		m.currentInspection = &deck.SessionInspection{
			Instance: deck.InstanceRecord{
				Title:     item.GroupNode.Name,
				GroupPath: item.GroupNode.Path,
				Status:    "group",
				Tool:      "group",
			},
			ActiveProfile: "group",
		}
		return nil
	}

	if !item.IsGroup && item.Session != nil {
		sess := item.Session

		// 1. Instant cache hit (0ms latency)
		if cached := deck.GetCachedInspection(sess.ID); cached != nil {
			m.currentInspection = cached
			return nil
		}

		// 2. Instantaneous fast in-memory inspection (0ms latency, zero I/O)
		m.currentInspection = deck.FastInspectionFromRecord(sess)

		// 3. Asynchronously fetch deep inspection / live terminal preview in background
		sdb := m.sdb
		if sdb == nil {
			return nil
		}
		targetID := sess.ID
		targetRecord := *sess
		return func() tea.Msg {
			insp, err := sdb.InspectSessionRecord(&targetRecord)
			if err == nil && insp != nil {
				return sessionInspectionLoadedMsg{
					sessionID:  targetID,
					inspection: insp,
				}
			}
			return nil
		}
	}

	return nil
}

func (m Model) Init() tea.Cmd {
	return m.updateHoveredInspection()
}

type attachFinishedMsg struct{ err error }

type sessionAttachReadyMsg struct {
	cmd *exec.Cmd
	err error
}

func (m Model) openSession(inst deck.InstanceRecord) (tea.Model, tea.Cmd) {
	m.setToast("Opening session...")
	return m, func() tea.Msg {
		if m.sdb == nil {
			return sessionAttachReadyMsg{err: fmt.Errorf("session database unavailable")}
		}
		cmd, err := m.sdb.PrepareSessionAttach(m.v, inst.ID)
		return sessionAttachReadyMsg{cmd: cmd, err: err}
	}
}

func (m *Model) initNewTaskDialog() {
	m.showNewTaskDialog = true
	m.newTaskFieldFocus = 0
	m.newTaskTitleInput.Clear()
	m.newTaskPromptInput.Clear()
	m.newTaskPathCycler.Reset()
	m.newTaskGroupCycler.Reset()
	m.newTaskToolIdx = 0
	m.newTaskSkillCursor = 0
	m.newTaskAccountIdx = 0
	m.newTaskAutoDeriveTitle = true
	if len(m.newTaskSkillProfiles) == 0 {
		m.newTaskSkillProfiles = map[string]bool{"core": true}
	}

	// Load recent paths and known groups from state.db
	if m.sdb != nil {
		if insts, err := m.sdb.LoadInstances(); err == nil {
			m.newTaskRecentPaths = GetRecentProjectPaths(insts)
		}
		if groups, err := m.sdb.LoadGroups(); err == nil {
			var groupPaths []string
			for _, g := range groups {
				groupPaths = append(groupPaths, g.Path)
			}
			m.newTaskKnownGroups = groupPaths
		}
	}
	m.newTaskRecentIdx = 0

	// Pre-fill path and group from current selection
	initialGroup := "my-sessions"
	initialPath, _ := os.Getwd()

	if len(m.treeItems) > 0 && m.selectedTreeIdx >= 0 && m.selectedTreeIdx < len(m.treeItems) {
		item := m.treeItems[m.selectedTreeIdx]
		if item.IsGroup && item.GroupNode != nil {
			initialGroup = item.GroupNode.Path
			if len(item.GroupNode.Sessions) > 0 && item.GroupNode.Sessions[0].ProjectPath != "" {
				initialPath = item.GroupNode.Sessions[0].ProjectPath
			}
		} else if !item.IsGroup && item.Session != nil {
			initialGroup = item.Session.GroupPath
			initialPath = item.Session.ProjectPath
			// Match account if possible
			for idx, acc := range m.accounts {
				if strings.EqualFold(acc.Email, item.Session.Account) || strings.EqualFold(acc.ID, item.Session.Account) {
					m.newTaskAccountIdx = idx
					break
				}
			}
			// Match tool if possible
			for idx, h := range availableHarnesses {
				if strings.EqualFold(h.ID, item.Session.Tool) {
					m.newTaskToolIdx = idx
					break
				}
			}
		}
	}
	if initialGroup == "" {
		initialGroup = "my-sessions"
	}
	if initialPath == "" {
		initialPath, _ = os.Getwd()
	}

	m.newTaskPathInput.Set(initialPath)
	m.newTaskGroupInput.Set(initialGroup)
	m.newTaskTitleInput.Clear() // Leave title empty initially for user input
	m.newTaskAutoDeriveTitle = true
	m.syncNewTaskStrings()
}

func (m *Model) syncNewTaskStrings() {
	m.newTaskTitle = m.newTaskTitleInput.Value
	m.newTaskPath = m.newTaskPathInput.Value
	m.newTaskGroup = m.newTaskGroupInput.Value
	m.newTaskPrompt = m.newTaskPromptInput.Value
}

func (m Model) submitNewTask() (tea.Model, tea.Cmd) {
	m.showNewTaskDialog = false
	m.newTaskPathCycler.Reset()
	m.newTaskGroupCycler.Reset()

	if m.sdb != nil {
		toolID := "agy"
		if m.newTaskToolIdx >= 0 && m.newTaskToolIdx < len(availableHarnesses) {
			toolID = availableHarnesses[m.newTaskToolIdx].ID
		}
		accountID := "default"
		if len(m.newTaskAccounts()) > 0 && m.newTaskAccountIdx >= 0 && m.newTaskAccountIdx < len(m.newTaskAccounts()) {
			accountID = m.newTaskAccounts()[m.newTaskAccountIdx].Email
			if accountID == "" {
				accountID = m.newTaskAccounts()[m.newTaskAccountIdx].ID
			}
		}
		var selectedProfiles []string
		for _, p := range m.skillProfileOptions() {
			if m.newTaskSkillProfiles != nil && m.newTaskSkillProfiles[p] {
				selectedProfiles = append(selectedProfiles, p)
			}
		}
		if len(selectedProfiles) == 0 {
			selectedProfiles = []string{"none"}
		}
		profile := strings.Join(selectedProfiles, ",")
		title := strings.TrimSpace(m.newTaskTitleInput.Value)
		if title == "" {
			title = strings.TrimSpace(m.newTaskTitle)
		}
		path := strings.TrimSpace(m.newTaskPathInput.Value)
		if path == "" {
			path = strings.TrimSpace(m.newTaskPath)
		}
		group := strings.TrimSpace(m.newTaskGroupInput.Value)
		if group == "" {
			group = strings.TrimSpace(m.newTaskGroup)
		}
		prompt := strings.TrimSpace(m.newTaskPromptInput.Value)
		if prompt == "" {
			prompt = strings.TrimSpace(m.newTaskPrompt)
		}

		opts := deck.CreateTaskOptions{
			Title:          title,
			ProjectPath:    path,
			GroupPath:      group,
			Tool:           toolID,
			Account:        accountID,
			AccountProfile: m.newTaskAccountProfile,
			SkillProfile:   profile,
			InitialPrompt:  prompt,
		}
		inst, err := m.sdb.CreateTask(m.v, opts)
		if err != nil {
			m.setToast(fmt.Sprintf("Task creation failed: %v", err))
		} else {
			m.setToast(fmt.Sprintf("✓ Created task %q [%s] in %s", inst.Title, inst.Tool, inst.GroupPath))
			m.reload()
			for i, item := range m.treeItems {
				if !item.IsGroup && item.Session != nil && item.Session.ID == inst.ID {
					m.selectedTreeIdx = i
					m.updateHoveredInspection()
					break
				}
			}
		}
	}
	return m, nil
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case attachFinishedMsg:
		deck.InvalidateInspectionCache("")
		m.reload()
		if msg.err != nil {
			m.setToast(fmt.Sprintf("Could not attach to session: %v", msg.err))
		} else {
			m.setToast("Detached from session.")
		}
		return m, nil

	case sessionAttachReadyMsg:
		if msg.err != nil {
			m.setToast(fmt.Sprintf("Could not open session: %v", msg.err))
			return m, nil
		}
		return m, tea.ExecProcess(msg.cmd, func(err error) tea.Msg {
			return attachFinishedMsg{err: err}
		})

	case sessionInspectionLoadedMsg:
		if len(m.treeItems) > 0 && m.selectedTreeIdx >= 0 && m.selectedTreeIdx < len(m.treeItems) {
			item := m.treeItems[m.selectedTreeIdx]
			if !item.IsGroup && item.Session != nil && item.Session.ID == msg.sessionID {
				m.currentInspection = msg.inspection
			}
		}
		return m, nil

	case tea.MouseMsg:
		return m.handleMouse(msg)

	case tea.KeyMsg:
		key := msg.String()
		if m.showSkillsOverlay {
			return m.updateSkills(msg)
		}

		// 1. Delete Confirmation Dialog Modal
		if m.showDeleteDialog {
			switch key {
			case "y", "Y":
				m.showDeleteDialog = false
				if m.deleteIsGroup {
					if m.sdb != nil {
						deleted, err := m.sdb.DeleteGroupCascade(m.deleteTarget)
						if err != nil {
							m.setToast(fmt.Sprintf("Cascade delete failed: %v", err))
						} else {
							m.setToast(fmt.Sprintf("✓ Group %q and all %d tasks deleted.", m.deleteTarget, len(deleted)))
							m.reload()
						}
					}
				} else {
					_ = exec.Command("agent-deck", "session", "remove", "--force", m.deleteTarget).Run()
					m.setToast("✓ Session removed.")
					m.reload()
				}
				return m, nil

			case "n", "N", "esc", "q":
				m.showDeleteDialog = false
				m.setToast("Deletion cancelled.")
				return m, nil
			}
			return m, nil
		}

		// 1b. Switch Harness Modal
		if m.showSwitchDialog {
			switch key {
			case "1":
				m.switchTargetToolIdx = 0
			case "2":
				m.switchTargetToolIdx = 1
			case "3":
				m.switchTargetToolIdx = 2
			case "4":
				m.switchTargetToolIdx = 3
			case "5":
				m.switchTargetToolIdx = 4
			case "up", "k":
				if m.switchTargetToolIdx > 0 {
					m.switchTargetToolIdx--
				}
			case "down", "j":
				if m.switchTargetToolIdx < len(availableHarnesses)-1 {
					m.switchTargetToolIdx++
				}
			case "h", "H":
				m.switchWithHandoff = !m.switchWithHandoff
			case "p", "P":
				m.switchPreserveSkills = !m.switchPreserveSkills
			case "m", "M", "f", "F", "tab":
				m.switchCreateNewTask = !m.switchCreateNewTask
			case "enter":
				m.showSwitchDialog = false
				if m.switchTargetInst != nil && m.sdb != nil {
					target := availableHarnesses[m.switchTargetToolIdx].ID
					opts := deck.SwitchOptions{
						TargetTool:     target,
						AccountID:      m.switchTargetInst.Account,
						IncludeHandoff: m.switchWithHandoff,
						PreserveSkills: m.switchPreserveSkills,
						CreateNewTask:  m.switchCreateNewTask,
					}
					res, err := deck.SwitchHarness(m.sdb, m.v, m.switchTargetInst.ID, opts)
					if err != nil {
						m.setToast(fmt.Sprintf("Switch failed: %v", err))
					} else {
						deck.InvalidateInspectionCache("")
						m.reload()
						if opts.CreateNewTask {
							m.setToast(fmt.Sprintf("✓ Created new task %q [%s] (handoff: %v)", res.Instance.Title, res.NewTool, res.HandoffPath != ""))
						} else {
							m.setToast(fmt.Sprintf("✓ Recreated %q with %s (fresh session, handoff: %v)", res.Instance.Title, res.NewTool, res.HandoffPath != ""))
						}
						return m, m.updateHoveredInspection()
					}
				}
				return m, nil
			case "esc", "q":
				m.showSwitchDialog = false
				m.setToast("Switch cancelled.")
				return m, nil
			}
			return m, nil
		}

		// 1c. Switch Account Modal
		if m.showAccountDialog {
			switch key {
			case "p", "f", "b":
				m.changeAccountProfile(key)
				return m, nil
			case "1", "2", "3", "4", "5", "6", "7", "8", "9":
				idx := int(key[0] - '1')
				if idx < len(m.accounts) {
					m.accountModalIdx = idx
				}
			case "up", "k":
				if m.accountModalIdx > 0 {
					m.accountModalIdx--
				}
			case "down", "j":
				if m.accountModalIdx < len(m.accounts)-1 {
					m.accountModalIdx++
				}
			case "r", "R":
				m.accountModalRespawn = !m.accountModalRespawn
			case "enter":
				m.showAccountDialog = false
				if m.accountTargetInst != nil && m.sdb != nil && len(m.accounts) > 0 {
					targetAcc := m.accounts[m.accountModalIdx].Email
					if targetAcc == "" {
						targetAcc = m.accounts[m.accountModalIdx].ID
					}
					m.selectedAccount = m.accountModalIdx
					updated, err := deck.SwitchAccount(m.sdb, m.v, m.accountTargetInst.ID, targetAcc, m.accountModalRespawn)
					if err != nil {
						m.setToast(fmt.Sprintf("Account switch failed: %v", err))
					} else {
						m.setToast(fmt.Sprintf("✓ Account for %q switched to %s (respawned: %v)", updated.Title, updated.Account, m.accountModalRespawn))
						m.reload()
					}
				}
				return m, nil
			case "esc", "q":
				m.showAccountDialog = false
				m.setToast("Account switch cancelled.")
				return m, nil
			}
			return m, nil
		}

		// 1d. New Task Modal
		if m.showNewTaskDialog {
			if key == "ctrl+g" {
				cfg, err := rotation.LoadAccountProfiles("")
				if err != nil {
					m.setToast(err.Error())
				} else {
					m.newTaskAccountProfile = nextProfile(m.newTaskAccountProfile, cfg)
				}
				return m, nil
			}
			m.syncNewTaskStrings()

			// Global hotkeys in New Task modal:
			// 1. Esc: Cancel
			if key == "esc" {
				m.showNewTaskDialog = false
				m.newTaskPathCycler.Reset()
				m.newTaskGroupCycler.Reset()
				m.setToast("Task creation cancelled.")
				return m, nil
			}

			// 2. Ctrl+S or Ctrl+Enter: Create task immediately from ANY field
			if key == "ctrl+s" || key == "ctrl+enter" {
				return m.submitNewTask()
			}

			// 3. Ctrl+R: Cycle recent project paths
			if key == "ctrl+r" && len(m.newTaskRecentPaths) > 0 {
				m.newTaskRecentIdx = (m.newTaskRecentIdx + 1) % len(m.newTaskRecentPaths)
				nextP := m.newTaskRecentPaths[m.newTaskRecentIdx]
				m.newTaskPathInput.Set(nextP)
				m.newTaskPathCycler.Reset()
				if m.newTaskAutoDeriveTitle {
					base := filepath.Base(nextP)
					if base != "/" && base != "." {
						m.newTaskTitleInput.Set(base)
					}
				}
				m.syncNewTaskStrings()
				m.setToast(fmt.Sprintf("Recent path [%d/%d]: %s", m.newTaskRecentIdx+1, len(m.newTaskRecentPaths), nextP))
				return m, nil
			}

			// 4. Tab: Autocomplete or advance focus
			if key == "tab" {
				if m.newTaskFieldFocus == 1 { // Project Path
					pVal := m.newTaskPathInput.Value
					expanded := pVal
					if strings.HasPrefix(pVal, "~") {
						home, _ := os.UserHomeDir()
						expanded = filepath.Join(home, strings.TrimPrefix(pVal, "~"))
					}
					info, err := os.Stat(expanded)
					isDir := err == nil && info.IsDir()
					isPartial := !isDir || strings.HasSuffix(pVal, string(os.PathSeparator))

					if m.newTaskPathCycler.IsActive() || isPartial {
						if m.newTaskPathCycler.IsActive() {
							nextP := m.newTaskPathCycler.Next()
							m.newTaskPathInput.Set(nextP)
							if m.newTaskAutoDeriveTitle && m.newTaskTitleInput.Value == "" {
								m.newTaskTitleInput.Set(filepath.Base(nextP))
							}
							m.syncNewTaskStrings()
							return m, nil
						}
						matches, _ := GetDirectoryCompletions(m.newTaskPathInput.Value)
						if len(matches) > 0 {
							m.newTaskPathCycler.SetMatches(matches)
							nextP := m.newTaskPathCycler.Next()
							m.newTaskPathInput.Set(nextP)
							if m.newTaskAutoDeriveTitle && m.newTaskTitleInput.Value == "" {
								m.newTaskTitleInput.Set(filepath.Base(nextP))
							}
							m.syncNewTaskStrings()
							return m, nil
						}
					}
				} else if m.newTaskFieldFocus == 2 { // Group Path
					if m.newTaskGroupCycler.IsActive() {
						nextG := m.newTaskGroupCycler.Next()
						m.newTaskGroupInput.Set(nextG)
						m.syncNewTaskStrings()
						return m, nil
					}
					isExact := false
					for _, g := range m.newTaskKnownGroups {
						if strings.EqualFold(g, m.newTaskGroupInput.Value) {
							isExact = true
							break
						}
					}
					if !isExact && m.newTaskGroupInput.Value != "" && m.newTaskGroupInput.Value != "my-sessions" {
						matches := GetGroupCompletions(m.newTaskGroupInput.Value, m.newTaskKnownGroups)
						if len(matches) > 0 {
							m.newTaskGroupCycler.SetMatches(matches)
							nextG := m.newTaskGroupCycler.Next()
							m.newTaskGroupInput.Set(nextG)
							m.syncNewTaskStrings()
							return m, nil
						}
					}
				}
				// Otherwise advance focus
				m.newTaskPathCycler.Reset()
				m.newTaskGroupCycler.Reset()
				m.newTaskFieldFocus = (m.newTaskFieldFocus + 1) % 7
				return m, nil
			}

			// 5. Shift+Tab: Cycle previous match or retreat focus
			if key == "shift+tab" || key == "backtab" {
				if m.newTaskFieldFocus == 1 && m.newTaskPathCycler.IsActive() {
					prevP := m.newTaskPathCycler.Prev()
					m.newTaskPathInput.Set(prevP)
					if m.newTaskAutoDeriveTitle {
						m.newTaskTitleInput.Set(filepath.Base(prevP))
					}
					m.syncNewTaskStrings()
					return m, nil
				}
				if m.newTaskFieldFocus == 2 && m.newTaskGroupCycler.IsActive() {
					prevG := m.newTaskGroupCycler.Prev()
					m.newTaskGroupInput.Set(prevG)
					m.syncNewTaskStrings()
					return m, nil
				}
				m.newTaskPathCycler.Reset()
				m.newTaskGroupCycler.Reset()
				m.newTaskFieldFocus = (m.newTaskFieldFocus + 6) % 7
				return m, nil
			}

			// 6. Ctrl+N / Ctrl+P: Next / Prev field (or cycler next / prev)
			if key == "ctrl+n" {
				if m.newTaskFieldFocus == 1 && m.newTaskPathCycler.IsActive() {
					m.newTaskPathInput.Set(m.newTaskPathCycler.Next())
					m.syncNewTaskStrings()
					return m, nil
				}
				m.newTaskFieldFocus = (m.newTaskFieldFocus + 1) % 7
				return m, nil
			}
			if key == "ctrl+p" {
				if m.newTaskFieldFocus == 1 && m.newTaskPathCycler.IsActive() {
					m.newTaskPathInput.Set(m.newTaskPathCycler.Prev())
					m.syncNewTaskStrings()
					return m, nil
				}
				m.newTaskFieldFocus = (m.newTaskFieldFocus + 6) % 7
				return m, nil
			}

			// 7. Enter:
			if key == "enter" {
				if m.newTaskFieldFocus == 1 && m.newTaskPathCycler.IsActive() {
					m.newTaskPathCycler.Reset()
					m.newTaskFieldFocus = 2
					return m, nil
				}
				if m.newTaskFieldFocus == 2 && m.newTaskGroupCycler.IsActive() {
					m.newTaskGroupCycler.Reset()
					m.newTaskFieldFocus = 3
					return m, nil
				}
				if m.newTaskFieldFocus < 6 {
					m.newTaskFieldFocus++
					return m, nil
				}
				return m.submitNewTask()
			}

			// 8. Field-specific keys:
			switch m.newTaskFieldFocus {
			case 0: // Title
				switch key {
				case "left":
					m.newTaskTitleInput.Left()
				case "right":
					m.newTaskTitleInput.Right()
				case "home", "ctrl+a":
					m.newTaskTitleInput.Home()
				case "end", "ctrl+e":
					m.newTaskTitleInput.End()
				case "ctrl+u":
					m.newTaskTitleInput.Clear()
				case "ctrl+w":
					m.newTaskTitleInput.DeleteWordBackwardPath()
				case "backspace":
					m.newTaskTitleInput.Backspace()
				case "delete":
					m.newTaskTitleInput.Delete()
				case "up":
					m.newTaskFieldFocus = 6
				case "down":
					m.newTaskFieldFocus = 1
				case " ":
					m.newTaskTitleInput.Insert(' ')
					m.newTaskAutoDeriveTitle = false
				default:
					if len(msg.Runes) > 0 {
						for _, r := range msg.Runes {
							m.newTaskTitleInput.Insert(r)
						}
						m.newTaskAutoDeriveTitle = false
					}
				}

			case 1: // Project Path
				switch key {
				case "left":
					m.newTaskPathInput.Left()
				case "right":
					m.newTaskPathInput.Right()
				case "home", "ctrl+a":
					m.newTaskPathInput.Home()
				case "end", "ctrl+e":
					m.newTaskPathInput.End()
				case "ctrl+u":
					m.newTaskPathInput.Clear()
					m.newTaskPathCycler.Reset()
				case "ctrl+w":
					m.newTaskPathInput.DeleteWordBackwardPath()
					m.newTaskPathCycler.Reset()
					if m.newTaskAutoDeriveTitle {
						m.newTaskTitleInput.Set(filepath.Base(m.newTaskPathInput.Value))
					}
				case "backspace":
					m.newTaskPathInput.Backspace()
					m.newTaskPathCycler.Reset()
					if m.newTaskAutoDeriveTitle {
						m.newTaskTitleInput.Set(filepath.Base(m.newTaskPathInput.Value))
					}
				case "delete":
					m.newTaskPathInput.Delete()
					m.newTaskPathCycler.Reset()
				case "up":
					if m.newTaskPathCycler.IsActive() {
						m.newTaskPathInput.Set(m.newTaskPathCycler.Prev())
					} else {
						m.newTaskFieldFocus = 0
					}
				case "down":
					if m.newTaskPathCycler.IsActive() {
						m.newTaskPathInput.Set(m.newTaskPathCycler.Next())
					} else {
						m.newTaskFieldFocus = 2
					}
				case " ":
					m.newTaskPathInput.Insert(' ')
					m.newTaskPathCycler.Reset()
				default:
					if len(msg.Runes) > 0 {
						for _, r := range msg.Runes {
							m.newTaskPathInput.Insert(r)
						}
						m.newTaskPathCycler.Reset()
						if m.newTaskAutoDeriveTitle {
							m.newTaskTitleInput.Set(filepath.Base(m.newTaskPathInput.Value))
						}
					}
				}

			case 2: // Group Path
				switch key {
				case "left":
					m.newTaskGroupInput.Left()
				case "right":
					m.newTaskGroupInput.Right()
				case "home", "ctrl+a":
					m.newTaskGroupInput.Home()
				case "end", "ctrl+e":
					m.newTaskGroupInput.End()
				case "ctrl+u":
					m.newTaskGroupInput.Clear()
					m.newTaskGroupCycler.Reset()
				case "ctrl+w":
					m.newTaskGroupInput.DeleteWordBackwardPath()
					m.newTaskGroupCycler.Reset()
				case "backspace":
					m.newTaskGroupInput.Backspace()
					m.newTaskGroupCycler.Reset()
				case "delete":
					m.newTaskGroupInput.Delete()
					m.newTaskGroupCycler.Reset()
				case "up":
					if m.newTaskGroupCycler.IsActive() {
						m.newTaskGroupInput.Set(m.newTaskGroupCycler.Prev())
					} else {
						m.newTaskFieldFocus = 1
					}
				case "down":
					if m.newTaskGroupCycler.IsActive() {
						m.newTaskGroupInput.Set(m.newTaskGroupCycler.Next())
					} else {
						m.newTaskFieldFocus = 3
					}
				case " ":
					m.newTaskGroupInput.Insert(' ')
					m.newTaskGroupCycler.Reset()
				default:
					if len(msg.Runes) > 0 {
						for _, r := range msg.Runes {
							m.newTaskGroupInput.Insert(r)
						}
						m.newTaskGroupCycler.Reset()
					}
				}

			case 3: // Harness Selector
				prevTool := availableHarnesses[m.newTaskToolIdx].ID
				if key == "left" || key == "h" {
					m.newTaskToolIdx = (m.newTaskToolIdx + len(availableHarnesses) - 1) % len(availableHarnesses)
				} else if key == "right" || key == "l" || key == " " || key == "space" || msg.Type == tea.KeySpace {
					m.newTaskToolIdx = (m.newTaskToolIdx + 1) % len(availableHarnesses)
				} else if key >= "1" && key <= fmt.Sprintf("%d", len(availableHarnesses)) {
					m.newTaskToolIdx = int(key[0] - '1')
				} else if key == "up" {
					m.newTaskFieldFocus = 2
				} else if key == "down" {
					m.newTaskFieldFocus = 4
				}
				newTool := availableHarnesses[m.newTaskToolIdx].ID
				if newTool != prevTool && newTool == "vox-studio" {
					if m.newTaskGroupInput.Value == "" || m.newTaskGroupInput.Value == "my-sessions" || !strings.HasPrefix(m.newTaskGroupInput.Value, "Creative") {
						m.newTaskGroupInput.Set("Creative/VideoStudio")
					}
					if m.newTaskSkillProfiles == nil {
						m.newTaskSkillProfiles = make(map[string]bool)
					}
					m.newTaskSkillProfiles["video"] = true
				}

			case 4: // Account Selector
				if len(m.newTaskAccounts()) > 0 {
					if key == "left" || key == "h" {
						m.newTaskAccountIdx = (m.newTaskAccountIdx + len(m.newTaskAccounts()) - 1) % len(m.newTaskAccounts())
					} else if key == "right" || key == "l" || key == " " {
						m.newTaskAccountIdx = (m.newTaskAccountIdx + 1) % len(m.newTaskAccounts())
					} else if key >= "1" && key <= "9" {
						idx := int(key[0] - '1')
						if idx < len(m.newTaskAccounts()) {
							m.newTaskAccountIdx = idx
						}
					}
				}
				if key == "up" {
					m.newTaskFieldFocus = 3
				} else if key == "down" {
					m.newTaskFieldFocus = 5
				}

			case 5: // Skill Profile Multi-Chip Selector
				if m.newTaskSkillProfiles == nil {
					m.newTaskSkillProfiles = map[string]bool{"core": true}
				}
				if key == "left" || key == "h" {
					m.newTaskSkillCursor = (m.newTaskSkillCursor + len(m.skillProfileOptions()) - 1) % len(m.skillProfileOptions())
				} else if key == "right" || key == "l" {
					m.newTaskSkillCursor = (m.newTaskSkillCursor + 1) % len(m.skillProfileOptions())
				} else if key == " " {
					prof := m.skillProfileOptions()[m.newTaskSkillCursor]
					m.newTaskSkillProfiles[prof] = !m.newTaskSkillProfiles[prof]
				} else if key >= "1" && key <= fmt.Sprintf("%d", len(m.skillProfileOptions())) {
					idx := int(key[0] - '1')
					if idx >= 0 && idx < len(m.skillProfileOptions()) {
						prof := m.skillProfileOptions()[idx]
						m.newTaskSkillProfiles[prof] = !m.newTaskSkillProfiles[prof]
					}
				} else if key == "up" {
					m.newTaskFieldFocus = 4
				} else if key == "down" {
					m.newTaskFieldFocus = 6
				}

			case 6: // Initial Prompt
				switch key {
				case "left":
					m.newTaskPromptInput.Left()
				case "right":
					m.newTaskPromptInput.Right()
				case "home", "ctrl+a":
					m.newTaskPromptInput.Home()
				case "end", "ctrl+e":
					m.newTaskPromptInput.End()
				case "ctrl+u":
					m.newTaskPromptInput.Clear()
				case "ctrl+w":
					m.newTaskPromptInput.DeleteWordBackwardPath()
				case "backspace":
					m.newTaskPromptInput.Backspace()
				case "delete":
					m.newTaskPromptInput.Delete()
				case "up":
					m.newTaskFieldFocus = 5
				case "down":
					m.newTaskFieldFocus = 0
				case " ":
					m.newTaskPromptInput.Insert(' ')
				default:
					if len(msg.Runes) > 0 {
						for _, r := range msg.Runes {
							m.newTaskPromptInput.Insert(r)
						}
					}
				}
			}

			m.syncNewTaskStrings()
			return m, nil
		}

		// 2. Full-Screen Session Inspector Modal
		if m.showInspector {
			switch key {
			case "esc", "i", "q":
				m.showInspector = false
				return m, nil
			case "enter", "a":
				if m.currentInspection != nil {
					m.showInspector = false
					return m.openSession(m.currentInspection.Instance)
				}
			}
			return m, nil
		}

		// 2b. Command Palette Modal ([?] or Ctrl+P)
		if m.showCommandPalette {
			switch key {
			case "esc", "q", "ctrl+p", "?":
				m.showCommandPalette = false
				return m, nil

			case "up", "k":
				m.commandPaletteIdx = (m.commandPaletteIdx + len(commandPaletteItems) - 1) % len(commandPaletteItems)
				return m, nil

			case "down", "j":
				m.commandPaletteIdx = (m.commandPaletteIdx + 1) % len(commandPaletteItems)
				return m, nil

			case "enter":
				m.showCommandPalette = false
				action := commandPaletteItems[m.commandPaletteIdx].ActionID
				switch action {
				case "attach":
					if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
						return m.openSession(*m.treeItems[m.selectedTreeIdx].Session)
					}
				case "new_task":
					m.initNewTaskDialog()
				case "routines":
					m.routinesList, _ = routines.LoadRoutines(m.sdb)
					m.showRoutinesDialog = true
				case "voice":
					m.voiceStatusCache, _ = voice.GetStatus()
					m.showVoiceDialog = true
				case "switch_harness":
					if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
						m.showSwitchDialog = true
						m.switchTargetInst = m.treeItems[m.selectedTreeIdx].Session
						m.switchWithHandoff = true
						m.switchPreserveSkills = true
						m.switchCreateNewTask = false
					} else {
						m.setToast("Select a session in the tree to switch its harness.")
					}
				case "switch_account":
					if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
						m.showAccountDialog = true
						m.accountTargetInst = m.treeItems[m.selectedTreeIdx].Session
						m.accountModalRespawn = true
					} else {
						m.setToast("Select a session in the tree to switch its account.")
					}
				case "inspect":
					if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
						insp, err := m.sdb.InspectSession(m.treeItems[m.selectedTreeIdx].Session.ID)
						if err == nil {
							m.currentInspection = insp
							m.showInspector = true
						}
					}
				case "skills":
					m.showSkillsOverlay = true
				case "tech_stack":
					go func() {
						rep, err := assistant.AnalyzeTechStack("https://x.ai")
						if err == nil {
							ast := assistant.NewVirtualAssistant(m.sdb, m.v, assistant.DefaultAssistantConfig())
							ast.Speak(fmt.Sprintf("SaaS scan complete for x.ai: Detected %d components.", len(rep.DetectedTech)))
						}
					}()
					m.setToast("⚡ Tech stack reverse-engineered for x.ai (run `ai-desk stack <url>` for any domain).")
				case "failover":
					if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
						sess := m.treeItems[m.selectedTreeIdx].Session
						updated, err := deck.AutoFailoverSession(m.sdb, m.v, sess.ID, "Manual command palette failover")
						if err != nil {
							m.setToast(fmt.Sprintf("Failover failed: %v", err))
						} else {
							m.setToast(fmt.Sprintf("✓ Session %q account failed over to %s!", updated.Title, updated.Account))
							m.reload()
						}
					}
				case "delete":
					if len(m.treeItems) > 0 {
						item := m.treeItems[m.selectedTreeIdx]
						m.showDeleteDialog = true
						if item.IsGroup && item.GroupNode != nil {
							m.deleteIsGroup = true
							m.deleteTarget = item.GroupNode.Path
							m.deletePrompt = fmt.Sprintf("Cascade delete group %q and ALL %d sessions?", item.GroupNode.Path, item.GroupNode.TotalTasks)
						} else if !item.IsGroup && item.Session != nil {
							m.deleteIsGroup = false
							m.deleteTarget = item.Session.ID
							m.deletePrompt = fmt.Sprintf("Delete session %q [%s]?", item.Session.Title, item.Session.ID)
						}
					}
				case "refresh":
					m.reload()
					m.setToast("State, tree & quota refreshed.")
				case "quit":
					return m, tea.Quit
				}
				return m, nil
			}
			return m, nil
		}

		// 2c. Routines Panel Modal ([r])
		if m.showRoutinesDialog {
			switch key {
			case "esc", "q":
				m.showRoutinesDialog = false
				return m, nil

			case "up", "k":
				if m.routinesSelectedIdx > 0 {
					m.routinesSelectedIdx--
				}
				return m, nil

			case "down", "j":
				if m.routinesSelectedIdx < len(m.routinesList)-1 {
					m.routinesSelectedIdx++
				}
				return m, nil

			case "enter", "x":
				if len(m.routinesList) > 0 && m.routinesSelectedIdx < len(m.routinesList) {
					r := m.routinesList[m.routinesSelectedIdx]
					sched := routines.NewScheduler(m.sdb, 1*time.Minute)
					_, _ = sched.TriggerNow(r.ID)
					m.setToast(fmt.Sprintf("✓ Triggered execution of routine %q.", r.Title))
				}
				return m, nil

			case "p", "P":
				if len(m.routinesList) > 0 && m.routinesSelectedIdx < len(m.routinesList) {
					r := m.routinesList[m.routinesSelectedIdx]
					newSt := "paused"
					if r.Status == "paused" {
						newSt = "active"
					}
					_ = routines.UpdateRoutineStatus(m.sdb, r.ID, newSt)
					m.routinesList, _ = routines.LoadRoutines(m.sdb)
					m.setToast(fmt.Sprintf("✓ Routine %q status updated to %s.", r.Title, newSt))
				}
				return m, nil

			case "d", "D":
				if len(m.routinesList) > 0 && m.routinesSelectedIdx < len(m.routinesList) {
					r := m.routinesList[m.routinesSelectedIdx]
					_ = routines.DeleteRoutine(m.sdb, r.ID)
					m.routinesList, _ = routines.LoadRoutines(m.sdb)
					if m.routinesSelectedIdx >= len(m.routinesList) && m.routinesSelectedIdx > 0 {
						m.routinesSelectedIdx--
					}
					m.setToast(fmt.Sprintf("✓ Deleted routine %q.", r.Title))
				}
				return m, nil
			}
			return m, nil
		}

		// 2d. Voice & Virtual Assistant Panel Modal ([v])
		if m.showVoiceDialog {
			switch key {
			case "esc", "q":
				m.showVoiceDialog = false
				return m, nil

			case "1", "enter":
				m.showVoiceDialog = false
				return m.triggerVoiceChat()

			case "2":
				m.showVoiceDialog = false
				sessName, err := voice.StartFastSession()
				if err != nil {
					m.setToast(fmt.Sprintf("Voice session failed: %v", err))
					return m, nil
				}
				c := deck.TmuxAttachCommand(sessName, "")
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					return attachFinishedMsg{err: err}
				})

			case "3":
				m.showVoiceDialog = false
				go func() {
					ast := assistant.NewVirtualAssistant(m.sdb, m.v, assistant.DefaultAssistantConfig())
					ast.Speak("AI Desk virtual assistant: all monitored workloads are active. Local neural speech engine ready.")
				}()
				m.setToast("🎙 Speaking status briefing via local neural TTS...")
				return m, nil

			case "4":
				bin := voice.FindVoxBinary()
				if bin != "" {
					_ = exec.Command(bin, "daemon", "start").Run()
					m.voiceStatusCache, _ = voice.GetStatus()
					m.setToast("✓ Resident voice daemon started.")
				} else {
					m.setToast("Vox binary not found in ~/.local/bin/vox")
				}
				return m, nil

			case "5":
				m.showVoiceDialog = false
				go func() {
					rep, err := assistant.AnalyzeTechStack("https://x.ai")
					if err == nil {
						ast := assistant.NewVirtualAssistant(m.sdb, m.v, assistant.DefaultAssistantConfig())
						ast.Speak(fmt.Sprintf("SaaS scan complete for x.ai: Detected %d components.", len(rep.DetectedTech)))
					}
				}()
				m.setToast("⚡ Tech stack reverse-engineered for x.ai (run `ai-desk stack <url>` for any domain).")
				return m, nil
			}
			return m, nil
		}

		// 3. Global Navigation Keys
		switch key {
		case "ctrl+c":
			return m, tea.Quit

		case "q":
			if m.showSkillsOverlay {
				m.showSkillsOverlay = false
				return m, nil
			}
			return m, tea.Quit

		case "s":
			m.showSkillsOverlay = !m.showSkillsOverlay
			if m.showSkillsOverlay {
				m.setToast("Skills Manager opened.")
			} else {
				m.setToast("Skills Manager closed.")
			}
			return m, nil

		case "i":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if !item.IsGroup && item.Session != nil {
					if m.sdb != nil {
						insp, err := m.sdb.InspectSession(item.Session.ID)
						if err == nil {
							m.currentInspection = insp
							m.showInspector = true
							return m, nil
						}
					}
				} else if item.IsGroup && item.GroupNode != nil {
					m.setToast(fmt.Sprintf("Group %q (%d tasks). Space to expand/collapse.", item.GroupNode.Path, item.GroupNode.TotalTasks))
				}
			}
			return m, nil

		case "d":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					m.showDeleteDialog = true
					m.deleteIsGroup = true
					m.deleteTarget = item.GroupNode.Path
					m.deletePrompt = fmt.Sprintf("Cascade delete group %q and ALL %d sessions inside it?", item.GroupNode.Path, item.GroupNode.TotalTasks)
				} else if !item.IsGroup && item.Session != nil {
					m.showDeleteDialog = true
					m.deleteIsGroup = false
					m.deleteTarget = item.Session.ID
					m.deletePrompt = fmt.Sprintf("Delete session %q [%s]?", item.Session.Title, item.Session.ID)
				}
			}
			return m, nil

		case "n", "N":
			m.initNewTaskDialog()
			return m, nil

		case "?", "ctrl+p":
			m.showCommandPalette = true
			return m, nil

		case "v":
			m.voiceStatusCache, _ = voice.GetStatus()
			m.showVoiceDialog = true
			return m, nil

		case "V":
			return m.triggerVoiceChat()

		case "f", "F":
			if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
				sess := m.treeItems[m.selectedTreeIdx].Session
				updated, err := deck.AutoFailoverSession(m.sdb, m.v, sess.ID, "Manual key shortcut failover")
				if err != nil {
					m.setToast(fmt.Sprintf("Failover failed: %v", err))
				} else {
					m.setToast(fmt.Sprintf("✓ Session %q account failed over to %s!", updated.Title, updated.Account))
					m.reload()
				}
			} else {
				m.setToast("Select a session in the tree to trigger failover.")
			}
			return m, nil

		case "S":
			go func() {
				rep, err := assistant.AnalyzeTechStack("https://x.ai")
				if err == nil {
					ast := assistant.NewVirtualAssistant(m.sdb, m.v, assistant.DefaultAssistantConfig())
					ast.Speak(fmt.Sprintf("SaaS scan complete for x.ai: Detected %d components.", len(rep.DetectedTech)))
				}
			}()
			m.setToast("⚡ Tech stack reverse-engineered for x.ai (run `ai-desk stack <url>` for any domain).")
			return m, nil

		case "w", "W":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if !item.IsGroup && item.Session != nil {
					m.showSwitchDialog = true
					m.switchTargetInst = item.Session
					m.switchWithHandoff = true
					m.switchPreserveSkills = true
					m.switchCreateNewTask = false
					m.switchTargetToolIdx = 0
					for idx, h := range availableHarnesses {
						if h.ID != strings.ToLower(item.Session.Tool) {
							m.switchTargetToolIdx = idx
							break
						}
					}
					return m, nil
				} else {
					m.setToast("Select a session in the tree to switch its agent harness.")
				}
			}
			return m, nil

		case "a", "A":
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if !item.IsGroup && item.Session != nil {
					m.accounts, _ = harness.ListAccounts(item.Session.Tool)
					if len(m.accounts) == 0 {
						m.accounts = []rotation.AccountStatus{{ID: "default", Email: "default", Provider: item.Session.Tool}}
					}
					m.showAccountDialog = true
					m.accountTargetInst = item.Session
					m.accountModalRespawn = true
					m.accountModalIdx = 0
					resAcc := deck.ResolveInstanceAccount(item.Session)
					targetMatch := resAcc.Email
					if targetMatch == "" {
						targetMatch = item.Session.Account
					}
					for idx, acc := range m.accounts {
						if strings.EqualFold(acc.Email, targetMatch) || strings.EqualFold(acc.ID, targetMatch) {
							m.accountModalIdx = idx
							break
						}
					}
					return m, nil
				} else {
					m.setToast("Select a session in the tree to switch its account.")
				}
			}
			return m, nil

		case "tab":
			if !m.showSkillsOverlay {
				m.focus = (m.focus + 1) % 3
			}
			return m, nil

		case "right", "l":
			if m.focus == FocusSelection && len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					if !item.GroupNode.Expanded {
						item.GroupNode.Expanded = true
						m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
						cmd := m.updateHoveredInspection()
						return m, cmd
					}
					// If already expanded, step into first child if any
					if m.selectedTreeIdx < len(m.treeItems)-1 {
						m.selectedTreeIdx++
						cmd := m.updateHoveredInspection()
						return m, cmd
					}
				} else {
					// On a session: jump forward to the next group
					for next := m.selectedTreeIdx + 1; next < len(m.treeItems); next++ {
						if m.treeItems[next].IsGroup {
							m.selectedTreeIdx = next
							cmd := m.updateHoveredInspection()
							return m, cmd
						}
					}
				}
			} else if !m.showSkillsOverlay {
				m.focus = (m.focus + 1) % 3
			}
			return m, nil

		case "left", "h":
			if m.focus == FocusSelection && len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					if item.GroupNode.Expanded {
						item.GroupNode.Expanded = false
						m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
						if m.selectedTreeIdx >= len(m.treeItems) {
							m.selectedTreeIdx = len(m.treeItems) - 1
						}
						cmd := m.updateHoveredInspection()
						return m, cmd
					}
					// If already collapsed, jump to parent group or previous group
					found := false
					for prev := m.selectedTreeIdx - 1; prev >= 0; prev-- {
						if m.treeItems[prev].IsGroup && m.treeItems[prev].Depth < item.Depth {
							m.selectedTreeIdx = prev
							cmd := m.updateHoveredInspection()
							return m, cmd
						}
					}
					if !found {
						for prev := m.selectedTreeIdx - 1; prev >= 0; prev-- {
							if m.treeItems[prev].IsGroup {
								m.selectedTreeIdx = prev
								cmd := m.updateHoveredInspection()
								return m, cmd
							}
						}
					}
					return m, nil
				} else {
					// On a session: jump to its parent group
					for prev := m.selectedTreeIdx - 1; prev >= 0; prev-- {
						if m.treeItems[prev].IsGroup && m.treeItems[prev].Depth < item.Depth {
							m.selectedTreeIdx = prev
							cmd := m.updateHoveredInspection()
							return m, cmd
						}
					}
					return m, nil
				}
			} else if !m.showSkillsOverlay {
				m.focus = (m.focus + 2) % 3
			}
			return m, nil

		case "up", "k":
			if m.showSkillsOverlay {
				if m.selectedProfile > 0 {
					m.selectedProfile--
				}
			} else if m.focus == FocusSelection {
				if len(m.treeItems) > 0 && m.selectedTreeIdx > 0 {
					m.selectedTreeIdx--
					m.taskScrollOffset = 0
					cmd := m.updateHoveredInspection()
					return m, cmd
				}
			} else if m.focus == FocusTask {
				m.taskScrollOffset++
			} else if m.focus == FocusStatus {
				if m.statusScrollOffset > 0 {
					m.statusScrollOffset--
				}
			}
			return m, nil

		case "down", "j":
			if m.showSkillsOverlay {
				if m.selectedProfile < len(m.profileKeys)-1 {
					m.selectedProfile++
				}
			} else if m.focus == FocusSelection {
				if len(m.treeItems) > 0 && m.selectedTreeIdx < len(m.treeItems)-1 {
					m.selectedTreeIdx++
					m.taskScrollOffset = 0
					cmd := m.updateHoveredInspection()
					return m, cmd
				}
			} else if m.focus == FocusTask {
				if m.taskScrollOffset > 0 {
					m.taskScrollOffset--
				}
			} else if m.focus == FocusStatus {
				m.statusScrollOffset++
			}
			return m, nil

		case "pgup":
			if m.focus == FocusTask {
				m.taskScrollOffset += 8
			}
			return m, nil

		case "pgdown":
			if m.focus == FocusTask {
				if m.taskScrollOffset > 8 {
					m.taskScrollOffset -= 8
				} else {
					m.taskScrollOffset = 0
				}
			}
			return m, nil

		case " ":
			// Toggle expand/collapse on group in Selection pane
			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					item.GroupNode.Expanded = !item.GroupNode.Expanded
					m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
					if m.selectedTreeIdx >= len(m.treeItems) {
						m.selectedTreeIdx = len(m.treeItems) - 1
					}
					cmd := m.updateHoveredInspection()
					return m, cmd
				}
			}
			return m, nil

		case "r":
			m.routinesList, _ = routines.LoadRoutines(m.sdb)
			m.showRoutinesDialog = true
			return m, nil

		case "R", "f5":
			m.reload()
			m.setToast("State, tree & quota refreshed.")
			return m, nil

		case "enter":
			if m.showSkillsOverlay {
				if len(m.profileKeys) > 0 && m.selectedProfile < len(m.profileKeys) {
					pName := m.profileKeys[m.selectedProfile]
					m.newTaskSkillProfiles = map[string]bool{pName: true}
					m.showSkillsOverlay = false
					m.setToast(fmt.Sprintf("✓ Skill profile %q activated for new sessions.", pName))
				}
				return m, nil
			}

			if len(m.treeItems) > 0 {
				item := m.treeItems[m.selectedTreeIdx]
				if item.IsGroup && item.GroupNode != nil {
					// Toggle expand/collapse
					item.GroupNode.Expanded = !item.GroupNode.Expanded
					m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
					if m.selectedTreeIdx >= len(m.treeItems) {
						m.selectedTreeIdx = len(m.treeItems) - 1
					}
					cmd := m.updateHoveredInspection()
					return m, cmd
				} else if !item.IsGroup && item.Session != nil {
					return m.openSession(*item.Session)
				}
			} else if len(m.sessions) > 0 {
				sess := m.sessions[m.selectedSession]
				c := deck.TmuxAttachCommand(sess.ID, "")
				return m, tea.ExecProcess(c, func(err error) tea.Msg {
					return attachFinishedMsg{err: err}
				})
			}
		}
	}

	return m, nil
}

func (m Model) triggerVoiceChat() (tea.Model, tea.Cmd) {
	var sessName string
	var err error
	if len(m.treeItems) > 0 && !m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].Session != nil {
		inst := m.treeItems[m.selectedTreeIdx].Session
		sessName, err = voice.StartAgentVoiceSession(inst.ID, inst.Title)
		if err != nil {
			m.setToast(fmt.Sprintf("Voice chat failed: %v", err))
			return m, nil
		}
		m.setToast(fmt.Sprintf("Connected Voice Chat to %q [%s]", inst.Title, inst.Tool))
	} else if len(m.treeItems) > 0 && m.treeItems[m.selectedTreeIdx].IsGroup && m.treeItems[m.selectedTreeIdx].GroupNode != nil && len(m.treeItems[m.selectedTreeIdx].GroupNode.Sessions) > 0 {
		inst := m.treeItems[m.selectedTreeIdx].GroupNode.Sessions[0]
		sessName, err = voice.StartAgentVoiceSession(inst.ID, inst.Title)
		if err != nil {
			m.setToast(fmt.Sprintf("Voice chat failed: %v", err))
			return m, nil
		}
		m.setToast(fmt.Sprintf("Connected Voice Chat to %q [%s]", inst.Title, inst.Tool))
	} else {
		sessName, err = voice.StartFastSession()
		if err != nil {
			m.setToast(fmt.Sprintf("Voice session failed: %v", err))
			return m, nil
		}
		m.setToast("Attached to Standby Voice Session")
	}

	c := deck.TmuxAttachCommand(sessName, "")
	return m, tea.ExecProcess(c, func(err error) tea.Msg {
		return attachFinishedMsg{err: err}
	})
}

func (m Model) clickedItemID(idx int) string {
	if idx < 0 || idx >= len(m.treeItems) {
		return ""
	}
	item := m.treeItems[idx]
	if item.IsGroup && item.GroupNode != nil {
		return "group:" + item.GroupNode.Path
	}
	if !item.IsGroup && item.Session != nil {
		return "session:" + item.Session.ID
	}
	return ""
}

func (m Model) mouseYToItemIndex(y int, contentHeight int) int {
	if len(m.treeItems) == 0 {
		return -1
	}

	// Main content starts at Y = 2 (border).
	// Header of Pane 1 is at Y = 3.
	// List items begin at Y = 4.
	if y < 4 {
		return -1
	}

	availLines := contentHeight - 3
	if availLines < 5 {
		availLines = 5
	}

	startIdx := m.treeScrollOffset
	endIdx := startIdx + availLines
	if endIdx > len(m.treeItems) {
		endIdx = len(m.treeItems)
	}

	lineInBody := y - 4

	// If scrolled down, line 0 is the "▲ (more above)" indicator
	if startIdx > 0 {
		if lineInBody == 0 {
			return -1 // Clicked on top scroll indicator
		}
		lineInBody--
	}

	renderedCount := endIdx - startIdx
	if lineInBody < 0 || lineInBody >= renderedCount {
		return -1 // Beyond visible item range
	}

	itemIndex := startIdx + lineInBody
	if itemIndex < 0 || itemIndex >= len(m.treeItems) {
		return -1
	}
	return itemIndex
}

func (m Model) mouseFallbackSessionIndex(y int) int {
	if len(m.sessions) == 0 || y < 4 {
		return -1
	}
	lineInBody := y - 4
	if lineInBody < 0 || lineInBody >= len(m.sessions) {
		return -1
	}
	return lineInBody
}

func (m Model) handleMouse(msg tea.MouseMsg) (tea.Model, tea.Cmd) {
	if m.width == 0 || m.height == 0 {
		return m, nil
	}

	availWidth := m.width - 6
	if availWidth < 80 {
		availWidth = 80
	}
	col1Width := (availWidth * 28) / 100
	if col1Width < 26 {
		col1Width = 26
	}
	col3Width := (availWidth * 30) / 100
	if col3Width < 28 {
		col3Width = 28
	}
	col2Width := availWidth - col1Width - col3Width
	if col2Width < 30 {
		col2Width = 30
	}

	pane1End := col1Width
	pane2Start := col1Width + 1
	pane2End := col1Width + 1 + col2Width
	pane3Start := col1Width + col2Width + 2

	// 1. Mouse Wheel Scrolling (routed by pointer hover X)
	if msg.Button == tea.MouseButtonWheelUp || msg.Button == tea.MouseButtonWheelDown {
		if msg.X < pane1End {
			if len(m.treeItems) > 0 {
				if msg.Button == tea.MouseButtonWheelUp && m.selectedTreeIdx > 0 {
					m.selectedTreeIdx--
					m.taskScrollOffset = 0
					cmd := m.updateHoveredInspection()
					return m, cmd
				} else if msg.Button == tea.MouseButtonWheelDown && m.selectedTreeIdx < len(m.treeItems)-1 {
					m.selectedTreeIdx++
					m.taskScrollOffset = 0
					cmd := m.updateHoveredInspection()
					return m, cmd
				}
			} else if len(m.sessions) > 0 {
				if msg.Button == tea.MouseButtonWheelUp && m.selectedSession > 0 {
					m.selectedSession--
					m.taskScrollOffset = 0
				} else if msg.Button == tea.MouseButtonWheelDown && m.selectedSession < len(m.sessions)-1 {
					m.selectedSession++
					m.taskScrollOffset = 0
				}
			}
			return m, nil
		}

		if msg.X >= pane2Start && msg.X < pane2End {
			if msg.Button == tea.MouseButtonWheelUp {
				m.taskScrollOffset++
			} else if msg.Button == tea.MouseButtonWheelDown {
				if m.taskScrollOffset > 0 {
					m.taskScrollOffset--
				}
			}
			return m, nil
		}

		return m, nil
	}

	// 2. Mouse Click
	if msg.Button == tea.MouseButtonLeft {
		if msg.Action != tea.MouseActionPress {
			return m, nil
		}

		if m.showDeleteDialog || m.showInspector || m.showSkillsOverlay || m.showSwitchDialog || m.showAccountDialog || m.showNewTaskDialog {
			return m, nil
		}

		contentHeight := m.height - 7
		if contentHeight < 10 {
			contentHeight = 10
		}

		// Clicks outside main content (top header or footer) are ignored
		if msg.Y < 2 || msg.Y >= 2+contentHeight {
			return m, nil
		}

		// A. Click inside Pane 1 (Selection tree)
		if msg.X < pane1End {
			wasAlreadyFocused := m.focus == FocusSelection
			m.focus = FocusSelection

			if len(m.treeItems) > 0 {
				clickedIdx := m.mouseYToItemIndex(msg.Y, contentHeight)
				if clickedIdx < 0 {
					// Check if clicked scroll indicator
					availLines := contentHeight - 3
					if availLines < 5 {
						availLines = 5
					}
					startIdx := m.treeScrollOffset
					endIdx := startIdx + availLines
					if endIdx > len(m.treeItems) {
						endIdx = len(m.treeItems)
					}
					lineInBody := msg.Y - 4

					if startIdx > 0 && lineInBody == 0 {
						if m.treeScrollOffset > 0 {
							m.treeScrollOffset--
							m.updateHoveredInspection()
						}
					} else {
						topOffset := 0
						if startIdx > 0 {
							topOffset = 1
						}
						renderedCount := endIdx - startIdx
						if endIdx < len(m.treeItems) && lineInBody == topOffset+renderedCount {
							if m.treeScrollOffset < len(m.treeItems)-availLines {
								m.treeScrollOffset++
								m.updateHoveredInspection()
							}
						}
					}
					return m, nil
				}

				now := time.Now()
				clickedID := m.clickedItemID(clickedIdx)
				isDoubleClick := clickedIdx == m.lastClickIdx &&
					clickedID != "" &&
					clickedID == m.lastClickID &&
					now.Sub(m.lastClickTime) < 400*time.Millisecond

				m.lastClickTime = now
				m.lastClickIdx = clickedIdx
				m.lastClickID = clickedID

				item := m.treeItems[clickedIdx]

				if item.IsGroup && item.GroupNode != nil {
					// Toggle expand/collapse if:
					// 1) Clicked on arrow/folder icon
					// 2) Double-click
					// 3) Already selected and clicked again
					isIconClick := msg.X <= 2+(item.Depth*2)+6
					if isIconClick || isDoubleClick || (m.selectedTreeIdx == clickedIdx && wasAlreadyFocused) {
						item.GroupNode.Expanded = !item.GroupNode.Expanded
						m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
						if m.selectedTreeIdx >= len(m.treeItems) {
							m.selectedTreeIdx = len(m.treeItems) - 1
						}
						m.updateHoveredInspection()
						return m, nil
					}

					// Otherwise single click selects group
					m.selectedTreeIdx = clickedIdx
					m.updateHoveredInspection()
					return m, nil

				} else if !item.IsGroup && item.Session != nil {
					// Session item:
					// Double-click OR clicking an already-selected session opens/attaches
					if isDoubleClick || (m.selectedTreeIdx == clickedIdx && wasAlreadyFocused) {
						m.lastClickIdx = -1 // Reset to prevent double attach
						return m.openSession(*item.Session)
					}

					// Single click selects session and updates preview/status
					m.selectedTreeIdx = clickedIdx
					m.taskScrollOffset = 0
					m.updateHoveredInspection()
					return m, nil
				}

			} else if len(m.sessions) > 0 {
				clickedIdx := m.mouseFallbackSessionIndex(msg.Y)
				if clickedIdx >= 0 && clickedIdx < len(m.sessions) {
					now := time.Now()
					clickedID := "session:" + m.sessions[clickedIdx].ID
					isDoubleClick := clickedIdx == m.lastClickIdx &&
						clickedID == m.lastClickID &&
						now.Sub(m.lastClickTime) < 400*time.Millisecond

					m.lastClickTime = now
					m.lastClickIdx = clickedIdx
					m.lastClickID = clickedID

					if isDoubleClick || (m.selectedSession == clickedIdx && wasAlreadyFocused) {
						m.lastClickIdx = -1
						sess := m.sessions[clickedIdx]
						c := deck.TmuxAttachCommand(sess.ID, "")
						return m, tea.ExecProcess(c, func(err error) tea.Msg {
							return attachFinishedMsg{err: err}
						})
					}

					m.selectedSession = clickedIdx
					m.taskScrollOffset = 0
					return m, nil
				}
			}
			return m, nil
		}

		// B. Click inside Pane 2 (The Task)
		if msg.X >= pane2Start && msg.X < pane2End {
			m.focus = FocusTask
			return m, nil
		}

		// C. Click inside Pane 3 (Session Status)
		if msg.X >= pane3Start {
			m.focus = FocusStatus
			_, voiceBtnIdx := m.buildStatusPaneLines(col3Width)
			if voiceBtnIdx >= 0 && msg.Y >= 4+voiceBtnIdx && msg.Y <= 4+voiceBtnIdx+1 {
				return m.triggerVoiceChat()
			}
			return m, nil
		}
	}

	return m, nil
}

func sbReadToken(sb *swapbridge.SwapBridge, email string) (string, error) {
	return sb.ReadAccountToken(email)
}

func (m *Model) setToast(msg string) {
	m.toast = msg
	m.toastExpiry = time.Now().Add(4 * time.Second)
}

func (m Model) View() string {
	if m.width == 0 {
		return "Loading AI Desk..."
	}

	// 1. Top Header
	header := m.renderHeader()

	// 2. Main Content
	contentHeight := m.height - 7
	if contentHeight < 10 {
		contentHeight = 10
	}

	var mainContent string
	if m.showDeleteDialog {
		mainContent = m.renderDeleteDialog(m.width-4, contentHeight)
	} else if m.showInspector && m.currentInspection != nil {
		mainContent = RenderInspectorView(m.currentInspection, m.width-4, contentHeight)
	} else if m.showSkillsOverlay {
		mainContent = m.renderSkillsOverlay(m.width-4, contentHeight)
	} else if m.showCommandPalette {
		mainContent = m.renderCommandPalette(m.width-4, contentHeight)
	} else if m.showRoutinesDialog {
		mainContent = m.renderRoutinesDialog(m.width-4, contentHeight)
	} else if m.showVoiceDialog {
		mainContent = m.renderVoiceDialog(m.width-4, contentHeight)
	} else {
		// 3-Pane Side-by-Side Layout
		availWidth := m.width - 6
		if availWidth < 80 {
			availWidth = 80
		}

		// Pane 1 (Selection): ~28%
		col1Width := (availWidth * 28) / 100
		if col1Width < 26 {
			col1Width = 26
		}

		// Pane 3 (Session Status): ~30%
		col3Width := (availWidth * 30) / 100
		if col3Width < 28 {
			col3Width = 28
		}

		// Pane 2 (The Task): remaining ~42%
		col2Width := availWidth - col1Width - col3Width
		if col2Width < 30 {
			col2Width = 30
		}

		pane1 := m.renderSelectionPane(col1Width, contentHeight)
		var pane2 string
		if m.showSwitchDialog {
			pane2 = m.renderSwitchDialog(col2Width, contentHeight)
		} else if m.showAccountDialog {
			pane2 = m.renderAccountDialog(col2Width, contentHeight)
		} else if m.showNewTaskDialog {
			pane2 = m.renderNewTaskDialog(col2Width, contentHeight)
		} else {
			pane2 = m.renderTaskPane(col2Width, contentHeight)
		}
		pane3 := m.renderStatusPane(col3Width, contentHeight)

		mainContent = lipgloss.JoinHorizontal(lipgloss.Top, pane1, " ", pane2, " ", pane3)
	}

	// 3. Footer / Shortcuts
	footer := m.renderFooter()

	return lipgloss.JoinVertical(lipgloss.Left, header, mainContent, footer)
}

func (m Model) renderHeader() string {
	rtkBadge := lipgloss.NewStyle().Foreground(subtleColor).Render("[RTK: -]")
	rtkClient := rtk.NewClient()
	if rtkClient.IsInstalled() {
		rtkBadge = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(successColor).Padding(0, 1).Render("RTK: Active")
	}
	title := titleStyle.Render("⚡ AI DESK") + badgeStyle.Render("v"+Version) + " " + rtkBadge
	activeInfo := ""
	var targetAcc rotation.AccountStatus
	hasTargetAcc := false
	if m.currentInspection != nil && m.currentInspection.Instance.Status != "group" {
		targetEmail := m.currentInspection.ResolvedAccount.Email
		if targetEmail != "" {
			for _, a := range m.accounts {
				if strings.EqualFold(a.Email, targetEmail) || strings.EqualFold(a.ID, targetEmail) {
					targetAcc = a
					hasTargetAcc = true
					break
				}
			}
		}
	}
	if !hasTargetAcc && len(m.accounts) > 0 {
		targetAcc = m.accounts[0]
		hasTargetAcc = true
	}
	if hasTargetAcc {
		bar := m.renderProgressBar(targetAcc.RemainingQuotaPct, 8)
		emailStr := truncateString(targetAcc.Email, 24)
		activeInfo = fmt.Sprintf("%s %3.0f%% [%s]", bar, targetAcc.RemainingQuotaPct, lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render(emailStr))
	} else if m.activeAccount != "" {
		activeInfo = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("Account: " + m.activeAccount)
	}

	toast := ""
	if time.Now().Before(m.toastExpiry) && m.toast != "" {
		toast = statusToastStyle.Render(m.toast)
	}

	gapWidth := m.width - lipgloss.Width(title) - lipgloss.Width(activeInfo) - lipgloss.Width(toast) - 4
	if gapWidth < 1 {
		gapWidth = 1
	}
	spacer := strings.Repeat(" ", gapWidth)

	topLine := lipgloss.JoinHorizontal(lipgloss.Center, title, spacer, toast, " ", activeInfo)
	return lipgloss.NewStyle().MarginBottom(1).Render(topLine)
}

// Pane 1: Selection (Hierarchical Tree of Groups & Tasks)
func (m Model) renderSelectionPane(width, height int) string {
	bStyle := inactiveBorder
	titleLeft := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("[1] SELECTION")
	titleRight := lipgloss.NewStyle().Foreground(subtleColor).Render("TREE")
	if m.focus == FocusSelection {
		bStyle = activeBorder
		titleLeft = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(primaryColor).Padding(0, 1).Render("1. SELECTION")
		titleRight = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("● ACTIVE")
	}

	title := titleLeft + " " + titleRight
	var lines []string

	if len(m.treeItems) == 0 && len(m.sessions) == 0 {
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No active groups or sessions."))
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Spawn one with: ai-desk run <tool>"))
	} else if len(m.treeItems) > 0 {
		availLines := height - 3
		if availLines < 5 {
			availLines = 5
		}

		// Adjust scroll offset to keep selected item in view
		if m.selectedTreeIdx < m.treeScrollOffset {
			m.treeScrollOffset = m.selectedTreeIdx
		}
		if m.selectedTreeIdx >= m.treeScrollOffset+availLines {
			m.treeScrollOffset = m.selectedTreeIdx - availLines + 1
		}
		if m.treeScrollOffset > len(m.treeItems)-availLines {
			m.treeScrollOffset = len(m.treeItems) - availLines
		}
		if m.treeScrollOffset < 0 {
			m.treeScrollOffset = 0
		}

		startIdx := m.treeScrollOffset
		endIdx := startIdx + availLines
		if endIdx > len(m.treeItems) {
			endIdx = len(m.treeItems)
		}

		// Top indicator if scrolled down
		if startIdx > 0 {
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(fmt.Sprintf("  ▲ (%d more above)", startIdx)))
		}

		for i := startIdx; i < endIdx; i++ {
			item := m.treeItems[i]
			indent := strings.Repeat("  ", item.Depth)
			isSelected := i == m.selectedTreeIdx

			if item.IsGroup && item.GroupNode != nil {
				prefix := "▼ 📂 "
				if !item.GroupNode.Expanded {
					prefix = "▶ 📁 "
				}
				countBadge := lipgloss.NewStyle().Foreground(subtleColor).Render(fmt.Sprintf("(%d)", item.GroupNode.TotalTasks))
				availNameLen := width - (item.Depth * 2) - 16
				if availNameLen < 6 {
					availNameLen = 6
				}
				nameStr := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(truncateString(item.GroupNode.Name, availNameLen))
				rowText := fmt.Sprintf("%s%s%s %s", indent, prefix, nameStr, countBadge)
				if isSelected {
					lines = append(lines, selectedRow.Width(width-4).Render("▎ "+rowText))
				} else {
					lines = append(lines, normalRow.Render("  "+rowText))
				}
			} else if !item.IsGroup && item.Session != nil {
				statusBullet := "●"
				bulletColor := subtleColor
				switch item.Session.Status {
				case "running":
					bulletColor = successColor
				case "waiting":
					bulletColor = warningColor
				case "error":
					bulletColor = errorColor
				}

				cachedInsp := deck.GetCachedInspection(item.Session.ID)
				isExhausted := cachedInsp != nil && cachedInsp.TokensUsed.Exhausted
				if isExhausted {
					statusBullet = "⚠"
					bulletColor = errorColor
				}

				bullet := lipgloss.NewStyle().Foreground(bulletColor).Render(statusBullet)
				toolBadge := RenderToolBadge(item.Session.Tool)
				if isExhausted {
					toolBadge = lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render("[429] ") + toolBadge
				}

				resAcc := deck.ResolveInstanceAccount(item.Session)
				accChip := ""
				if resAcc.Email != "" && width >= 38 {
					shortAcc := strings.Split(resAcc.Email, "@")[0]
					if len(shortAcc) > 10 {
						shortAcc = shortAcc[:10]
					}
					chipColor := cyanColor
					if resAcc.IsDefault {
						chipColor = subtleColor
					}
					accChip = " " + lipgloss.NewStyle().Foreground(chipColor).Render("@"+shortAcc)
				}

				availTitleLen := width - (item.Depth * 2) - 18 - lipgloss.Width(accChip)
				if isExhausted {
					availTitleLen -= 6
				}
				if availTitleLen < 8 {
					availTitleLen = 8
				}
				titleStr := truncateString(item.Session.Title, availTitleLen)
				rowContent := fmt.Sprintf("%s%s %-*s %s%s", indent, bullet, availTitleLen, titleStr, toolBadge, accChip)

				if isSelected {
					lines = append(lines, selectedRow.Width(width-4).Render("▎ "+rowContent))
				} else {
					lines = append(lines, normalRow.Render("  "+rowContent))
				}
			}
		}

		// Bottom indicator if more items below
		if endIdx < len(m.treeItems) {
			lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(fmt.Sprintf("  ▼ (%d more below)", len(m.treeItems)-endIdx)))
		}
	} else {
		for i, s := range m.sessions {
			badge := lipgloss.NewStyle().Foreground(subtleColor).Render("[IDLE]")
			if s.Attached {
				badge = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[ATTACHED]")
			}
			titleStr := truncateString(s.Title, width-20)
			rowContent := fmt.Sprintf("%-16s %s", titleStr, badge)
			if i == m.selectedSession {
				lines = append(lines, selectedRow.Width(width-4).Render("▎ "+rowContent))
			} else {
				lines = append(lines, normalRow.Render("  "+rowContent))
			}
		}
	}

	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

// Pane 2: The Task (Live Terminal Capture Preview)
func (m Model) renderTaskPane(width, height int) string {
	bStyle := inactiveBorder
	titleLeft := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("[2] THE TASK")
	titleRight := lipgloss.NewStyle().Foreground(successColor).Render("LIVE")
	if m.focus == FocusTask {
		bStyle = activeBorder
		titleLeft = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(primaryColor).Padding(0, 1).Render("2. THE TASK")
		titleRight = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("● ACTIVE")
	}

	var title string
	var lines []string

	if len(m.treeItems) == 0 {
		title = titleLeft + " " + titleRight + " " + lipgloss.NewStyle().Foreground(subtleColor).Render("(No Active Session)")
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No session selected."))
	} else {
		item := m.treeItems[m.selectedTreeIdx]

		if item.IsGroup && item.GroupNode != nil {
			title = fmt.Sprintf("%s %s  Group: %s", titleLeft, titleRight, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render(item.GroupNode.Name))
			lines = append(lines, cardHeader.Render("GROUP OVERVIEW"))
			lines = append(lines, fmt.Sprintf("  • Full Path:       %s", lipgloss.NewStyle().Foreground(cyanColor).Render(item.GroupNode.Path)))
			lines = append(lines, fmt.Sprintf("  • Total Sessions:  %d tasks", item.GroupNode.TotalTasks))
			lines = append(lines, fmt.Sprintf("  • Subgroups:       %d nested", len(item.GroupNode.Children)))
			lines = append(lines, fmt.Sprintf("  • Direct Tasks:    %d", len(item.GroupNode.Sessions)))
			lines = append(lines, "")
			lines = append(lines, cardHeader.Render("GROUP SHORTCUTS"))
			lines = append(lines, "  • [Space] / [Enter] : Toggle expand/collapse")
			lines = append(lines, "  • [d]               : Cascade delete group and ALL sessions")
			lines = append(lines, "  • [g]               : Create new subgroup")
		} else if !item.IsGroup && item.Session != nil {
			toolBadge := RenderToolBadge(item.Session.Tool)
			statusColor := successColor
			if item.Session.Status == "waiting" {
				statusColor = warningColor
			} else if item.Session.Status == "error" {
				statusColor = errorColor
			}
			statusTag := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(strings.ToUpper(item.Session.Status))

			resAcc := deck.ResolveInstanceAccount(item.Session)
			accBadge := ""
			if resAcc.DisplayName != "" {
				accColor := cyanColor
				if resAcc.IsDefault {
					accColor = subtleColor
				}
				accBadge = " " + lipgloss.NewStyle().Foreground(accColor).Bold(true).Render("[@"+resAcc.DisplayName+"]")
			}
			title = fmt.Sprintf("%s %s  %s %s%s [%s]", titleLeft, titleRight, item.Session.Title, toolBadge, accBadge, statusTag)

			if m.currentInspection != nil && len(m.currentInspection.TerminalPreview) > 0 {
				preview := m.currentInspection.TerminalPreview
				availLines := height - 4
				if availLines < 4 {
					availLines = 4
				}

				totalLines := len(preview)
				endIdx := totalLines - m.taskScrollOffset
				if endIdx > totalLines {
					endIdx = totalLines
				}
				if endIdx < availLines {
					endIdx = availLines
				}
				startIdx := endIdx - availLines
				if startIdx < 0 {
					startIdx = 0
				}

				if m.taskScrollOffset > 0 {
					lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render(fmt.Sprintf("[↑ History Scroll -%d lines (pgdn to reset)]", m.taskScrollOffset)))
				}

				for i := startIdx; i < endIdx && i < totalLines; i++ {
					l := preview[i]
					maxCol := width - 6
					if maxCol < 10 {
						maxCol = 10
					}
					l = truncateVisibleLine(l, maxCol)
					lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("│ ")+l)
				}
			} else {
				lines = append(lines, "")
				lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(cyanColor).Render("  ⚡ Interactive Terminal Ready"))
				lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  Session process is active inside tmux background pane."))
				lines = append(lines, "")
				lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("  Quick Actions:"))
				lines = append(lines, fmt.Sprintf("    • %s  Attach to live interactive tmux terminal", lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("[Enter]")))
				lines = append(lines, fmt.Sprintf("    • %s      Switch active AI harness (agy/claude/codex/opencode)", lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("[w]")))
				lines = append(lines, fmt.Sprintf("    • %s      Terminate & delete this session", lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("[d]")))
				lines = append(lines, fmt.Sprintf("    • %s      Inspect tokens & skill diagnostics", lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("[i]")))
			}
		}
	}

	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

// buildStatusPaneLines builds the content lines for Pane 3 and returns the line index of the Voice button.
func (m Model) buildStatusPaneLines(width int) ([]string, int) {
	var lines []string

	divWidth := width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth))

	if m.currentInspection != nil && m.currentInspection.Instance.Status != "group" {
		insp := m.currentInspection

		// 1. Session Identity Card
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ SESSION IDENTITY"))
		lines = append(lines, fmt.Sprintf("  ID:       %s", lipgloss.NewStyle().Foreground(subtleColor).Render(truncateString(insp.Instance.ID, width-14))))
		lines = append(lines, fmt.Sprintf("  Title:    %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(truncateString(insp.Instance.Title, width-14))))

		toolBadge := RenderToolBadge(insp.Instance.Tool)
		switchHist := ""
		if insp.Instance.ToolData != "" {
			var td map[string]interface{}
			if err := json.Unmarshal([]byte(insp.Instance.ToolData), &td); err == nil {
				if hist, ok := td["cross_harness_history"].([]interface{}); ok && len(hist) > 0 {
					last := hist[len(hist)-1].(map[string]interface{})
					if fromTool, ok := last["from"].(string); ok {
						switchHist = fmt.Sprintf(" %s %s", lipgloss.NewStyle().Foreground(subtleColor).Render("←"), RenderToolBadge(fromTool))
					}
				}
			}
		}
		lines = append(lines, fmt.Sprintf("  Harness:  %s%s", toolBadge, switchHist))
		if insp.Instance.GroupPath != "" {
			lines = append(lines, fmt.Sprintf("  Group:    %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(truncateString(insp.Instance.GroupPath, width-14))))
		}
		if insp.Instance.ProjectPath != "" {
			lines = append(lines, fmt.Sprintf("  Path:     %s", lipgloss.NewStyle().Foreground(subtleColor).Render(truncateString(insp.Instance.ProjectPath, width-14))))
		}
		if insp.Instance.TmuxSession != "" {
			lines = append(lines, fmt.Sprintf("  Tmux:     %s", lipgloss.NewStyle().Foreground(subtleColor).Render(truncateString(insp.Instance.TmuxSession, width-14))))
		}
		resAcc := insp.ResolvedAccount
		if resAcc.DisplayName == "" {
			resAcc = deck.ResolveInstanceAccount(&insp.Instance)
		}
		accDisplay := resAcc.DisplayName
		if accDisplay == "" {
			accDisplay = "default"
		}
		lines = append(lines, fmt.Sprintf("  Account:  %s", lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(truncateString(accDisplay, width-14))))
		if insp.Uptime > 0 {
			lines = append(lines, fmt.Sprintf("  Uptime:   %s", lipgloss.NewStyle().Foreground(successColor).Render(insp.Uptime.Round(time.Second).String())))
		}
		lines = append(lines, divider)

		// 2. Active Skill Profile & Prompt Tax Card
		taxColor := successColor
		if insp.PromptTaxTurn > 2000 {
			taxColor = errorColor
		} else if insp.PromptTaxTurn > 800 {
			taxColor = warningColor
		}
		taxBadge := lipgloss.NewStyle().Foreground(taxColor).Bold(true).Render(fmt.Sprintf("~%d tok/turn", insp.PromptTaxTurn))

		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ SKILLS & PROMPT TAX"))
		lines = append(lines, fmt.Sprintf("  Profile:  %s  %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(insp.ActiveProfile), taxBadge))
		if len(insp.ActiveSkills) > 0 {
			skillsSummary := strings.Join(insp.ActiveSkills, ", ")
			if len(skillsSummary) > width-14 {
				skillsSummary = skillsSummary[:width-17] + "..."
			}
			lines = append(lines, fmt.Sprintf("  Skills:   %s", lipgloss.NewStyle().Foreground(subtleColor).Render(skillsSummary)))
		}
		lines = append(lines, divider)

		// 3. Token & Quota Telemetry Card
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ TOKEN TELEMETRY & QUOTA"))
		if insp.HasTokenMetrics {
			// A. Prominent Exhaustion Warning if 429 / limit hit
			if insp.TokensUsed.Exhausted {
				alertBadge := lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("#ffffff")).
					Background(errorColor).
					Padding(0, 1).
					Render("⚠ QUOTA EXHAUSTED: " + insp.TokensUsed.ExhaustionReason)
				lines = append(lines, alertBadge)
				if !insp.TokensUsed.RateLimitResetAt.IsZero() {
					until := time.Until(insp.TokensUsed.RateLimitResetAt)
					resetStr := insp.TokensUsed.RateLimitResetAt.Format("Jan 02 15:04")
					if until > 0 {
						days := int(until.Hours()) / 24
						hours := int(until.Hours()) % 24
						lines = append(lines, fmt.Sprintf("  Resets:   %s (%dd %dh remaining)", lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render(resetStr), days, hours))
					} else {
						lines = append(lines, fmt.Sprintf("  Resets:   %s", resetStr))
					}
				}
				lines = append(lines, lipgloss.NewStyle().Foreground(accentColor).Render("  Action:   Press [w] to switch session harness to AGY"))
			}

			// B. Quota / Rate Limit Meter Bar
			if insp.TokensUsed.RateLimitUsedPct > 0 {
				remPct := 100.0 - insp.TokensUsed.RateLimitUsedPct
				if remPct < 0 {
					remPct = 0
				}
				limitBar := m.renderProgressBar(remPct, 10)
				plan := insp.TokensUsed.PlanType
				if plan == "" {
					plan = "quota"
				}
				limitColor := successColor
				if insp.TokensUsed.RateLimitUsedPct >= 90.0 {
					limitColor = errorColor
				} else if insp.TokensUsed.RateLimitUsedPct >= 70.0 {
					limitColor = warningColor
				}
				lines = append(lines, fmt.Sprintf("  Limit:    %s %s (%s)", limitBar, lipgloss.NewStyle().Foreground(limitColor).Bold(true).Render(fmt.Sprintf("%.1f%% used", insp.TokensUsed.RateLimitUsedPct)), plan))
			}

			// C. Tokens Count
			if insp.TokensUsed.TotalTokens > 0 {
				lines = append(lines, fmt.Sprintf("  Total:    %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(formatNumber(insp.TokensUsed.TotalTokens))))
				lines = append(lines, fmt.Sprintf("  In/Out:   %s / %s", formatNumber(insp.TokensUsed.InputTokens), formatNumber(insp.TokensUsed.OutputTokens)))
				if insp.TokensUsed.CachedInputTokens > 0 && insp.TokensUsed.InputTokens > 0 {
					cachePct := float64(insp.TokensUsed.CachedInputTokens) / float64(insp.TokensUsed.InputTokens) * 100.0
					lines = append(lines, fmt.Sprintf("  Cached:   %s (%s)", formatNumber(insp.TokensUsed.CachedInputTokens), lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(fmt.Sprintf("%.1f%%", cachePct))))
				}
			}

			// D. Context Window Utilization
			if insp.TokensUsed.ModelContextWindow > 0 {
				ctxPct := float64(insp.TokensUsed.InputTokens) / float64(insp.TokensUsed.ModelContextWindow) * 100.0
				lines = append(lines, fmt.Sprintf("  Context:  %s / %s (%.1f%%)", formatNumber(insp.TokensUsed.InputTokens), formatNumber(insp.TokensUsed.ModelContextWindow), ctxPct))
			}

			if insp.TokensUsed.AccountEmail != "" {
				lines = append(lines, fmt.Sprintf("  Account:  %s", lipgloss.NewStyle().Foreground(cyanColor).Render(truncateString(insp.TokensUsed.AccountEmail, width-14))))
			}
		} else {
			lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  No turn metrics in session log."))
		}

		// 4. RTK Savings Card
		if insp.RTKSavings != nil && insp.RTKSavings.TotalCommands > 0 {
			lines = append(lines, divider)
			rtkTag := lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(fmt.Sprintf("%s saved (%.1f%%)", formatNumber(int64(insp.RTKSavings.TotalSaved)), insp.RTKSavings.AvgSavingsPct*100.0))
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ RTK SAVINGS"))
			lines = append(lines, fmt.Sprintf("  Saved:    %s", rtkTag))
			lines = append(lines, fmt.Sprintf("  Commands: %d intercepted", insp.RTKSavings.TotalCommands))
		}

		// 5. Active Quota Card
		if len(m.accounts) > 0 {
			lines = append(lines, divider)
			var acc rotation.AccountStatus
			found := false
			targetEmail := insp.ResolvedAccount.Email
			if targetEmail == "" {
				targetEmail = insp.Instance.Account
			}
			for _, a := range m.accounts {
				if strings.EqualFold(a.Email, targetEmail) || strings.EqualFold(a.ID, targetEmail) {
					acc = a
					found = true
					break
				}
			}
			if !found {
				acc = m.accounts[0]
			}
			bar := m.renderProgressBar(acc.RemainingQuotaPct, 12)
			accHeader := acc.Email
			if accHeader == "" {
				accHeader = acc.ID
			}
			lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ ACTIVE QUOTA ("+truncateString(accHeader, width-18)+")"))
			lines = append(lines, fmt.Sprintf("  %s %4.0f%%", bar, acc.RemainingQuotaPct))
		}

	} else {
		// Group overview in status pane
		lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ GROUP ANALYTICS"))
		if len(m.treeItems) > 0 {
			item := m.treeItems[m.selectedTreeIdx]
			if item.GroupNode != nil {
				lines = append(lines, fmt.Sprintf("  Name:     %s", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(item.GroupNode.Name)))
				lines = append(lines, fmt.Sprintf("  Tasks:    %s", lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render(fmt.Sprintf("%d total", item.GroupNode.TotalTasks))))
				lines = append(lines, fmt.Sprintf("  Path:     %s", lipgloss.NewStyle().Foreground(subtleColor).Render(item.GroupNode.Path)))
				lines = append(lines, divider)
				lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("◆ ACTIONS"))
				lines = append(lines, "  • [Space] Expand / Collapse")
				lines = append(lines, "  • [d]     Cascade Delete")
				lines = append(lines, "  • [g]     New Subgroup")
			}
		}
		lines = append(lines, divider)
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Select a task session to inspect active skills, tokens used, and live telemetry."))
	}

	// Resident Voice Agent Bridge Card
	lines = append(lines, divider)
	targetTitle := "Standby (Fast Mode)"
	targetTool := "Voice"
	isTargetExhausted := false
	if m.currentInspection != nil && m.currentInspection.Instance.Status != "group" && m.currentInspection.Instance.Title != "" {
		targetTitle = truncateString(m.currentInspection.Instance.Title, width-20)
		targetTool = m.currentInspection.Instance.Tool
		isTargetExhausted = m.currentInspection.TokensUsed.Exhausted
	}

	btnText := fmt.Sprintf("[V] 🎙 VOICE CHAT: %s", targetTitle)
	btnBg := lipgloss.Color("#bb9af7")
	pairingStatus := lipgloss.NewStyle().Foreground(successColor).Render("● READY")
	if isTargetExhausted {
		btnText = fmt.Sprintf("[V] 🎙 VOICE CHAT: %s [EXHAUSTED]", targetTitle)
		btnBg = lipgloss.Color("#f7768e")
		pairingStatus = lipgloss.NewStyle().Foreground(errorColor).Bold(true).Render("⚠ QUOTA HIT (Switch [w])")
	}

	voiceBtn := lipgloss.NewStyle().
		Bold(true).
		Foreground(lipgloss.Color("#1a1b26")).
		Background(btnBg).
		Padding(0, 1).
		Render(btnText)
	voiceBtnIdx := len(lines)
	lines = append(lines, voiceBtn)
	lines = append(lines, fmt.Sprintf("  Pairing:  %s  %s", lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render(targetTool), pairingStatus))
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  Resident: Whisper (ASR) ⇄ Pocket-TTS (TTS)"))

	return lines, voiceBtnIdx
}

// Pane 3: Per-Session Status (Live Telemetry & Diagnostics)
func (m Model) renderStatusPane(width, height int) string {
	bStyle := inactiveBorder
	titleLeft := lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("[3] SESSION STATUS")
	titleRight := lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("LIVE")
	if m.focus == FocusStatus {
		bStyle = activeBorder
		titleLeft = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(primaryColor).Padding(0, 1).Render("3. SESSION STATUS")
		titleRight = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("● ACTIVE")
	}

	title := titleLeft + " " + titleRight
	lines, _ := m.buildStatusPaneLines(width)
	availHeight := height - 3
	if availHeight < 5 {
		availHeight = 5
	}
	if len(lines) > availHeight {
		start := m.statusScrollOffset
		if start > len(lines)-availHeight {
			start = len(lines) - availHeight
		}
		if start < 0 {
			start = 0
		}
		end := start + availHeight
		if end > len(lines) {
			end = len(lines)
		}
		lines = lines[start:end]
	}
	body := strings.Join(lines, "\n")
	return bStyle.Width(width).Height(height).Render(
		lipgloss.JoinVertical(lipgloss.Left, headerStyle.Render(title), body),
	)
}

func (m Model) renderDeleteDialog(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(errorColor).
		Padding(1, 2)

	var lines []string
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(errorColor).Render("⚠ CONFIRM CASCADE DELETION"))
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#FFFFFF")).Bold(true).Render(m.deletePrompt))
	lines = append(lines, "")
	if m.deleteIsGroup {
		lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("This will terminate all running tmux processes and delete all tasks in this group and all its subgroups."))
	} else {
		lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("This will terminate the session's tmux process and remove it from the registry."))
	}
	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Bold(true).Render("[y] Confirm & Delete   •   [n/Esc] Cancel"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderSwitchDialog(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(primaryColor).
		Padding(1, 2)

	var lines []string
	sessTitle := ""
	currTool := ""
	groupPath := ""
	if m.switchTargetInst != nil {
		sessTitle = truncateString(m.switchTargetInst.Title, width-14)
		currTool = m.switchTargetInst.Tool
		groupPath = m.switchTargetInst.GroupPath
	}

	divWidth := width - 8
	if divWidth < 10 {
		divWidth = 10
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth))

	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("🔄 Switch Agent for \"" + sessTitle + "\"")
	lines = append(lines, header)

	subInfo := fmt.Sprintf("Current Harness: %s", RenderToolBadge(currTool))
	if groupPath != "" {
		subInfo += fmt.Sprintf("   •   Group: %s", lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(groupPath))
	}
	lines = append(lines, subInfo)
	lines = append(lines, divider)

	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("Select Target Harness:"))

	for i, h := range availableHarnesses {
		isTarget := i == m.switchTargetToolIdx
		isCurrent := m.switchTargetInst != nil && strings.ToLower(m.switchTargetInst.Tool) == h.ID

		badge := ""
		if isTarget && (h.ID == "agy" || h.ID == "claude") {
			badge = " " + lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("[Recommended]")
		} else if isCurrent {
			badge = " " + lipgloss.NewStyle().Foreground(warningColor).Render("[Current]")
		}

		if isTarget {
			rowStr := fmt.Sprintf("▶ [%d] %-30s%s", i+1, h.Name, badge)
			lines = append(lines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(bgHighlight).
				Padding(0, 1).
				Width(divWidth).
				Render(rowStr))
		} else {
			rowStr := fmt.Sprintf("  [%d] %-30s%s", i+1, h.Name, badge)
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a9b1d6")).
				Padding(0, 1).
				Render(rowStr))
		}
	}

	lines = append(lines, divider)

	// Mode Toggle: Recreate Session vs Fork New Task
	modeRecreate := lipgloss.NewStyle().Foreground(subtleColor).Render("[ ] Recreate Session (In-Place)")
	modeFork := lipgloss.NewStyle().Foreground(subtleColor).Render("[ ] Fork New Task (Branch)")
	if !m.switchCreateNewTask {
		modeRecreate = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("[●] Recreate Session (Clean fresh start)")
	} else {
		modeFork = lipgloss.NewStyle().Foreground(accentColor).Bold(true).Render("[●] Fork New Task (Keep old task running)")
	}
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("Switch Mode:  ")+lipgloss.NewStyle().Foreground(subtleColor).Render("[m/f/Tab to toggle]"))
	lines = append(lines, fmt.Sprintf("  %s   •   %s", modeRecreate, modeFork))

	lines = append(lines, divider)

	// Context & Skills Toggles
	hCheck := lipgloss.NewStyle().Foreground(subtleColor).Render("[ ]")
	if m.switchWithHandoff {
		hCheck = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[✓]")
	}
	hDesc := fmt.Sprintf("  %s Synthesize Context Handoff Brief", hCheck)
	if m.switchWithHandoff {
		hDesc = fmt.Sprintf("  %s %s", hCheck, lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("Synthesize Context Handoff Brief"))
	}
	lines = append(lines, fmt.Sprintf("%-42s %s", hDesc, lipgloss.NewStyle().Foreground(subtleColor).Render("[h to toggle]")))

	pCheck := lipgloss.NewStyle().Foreground(subtleColor).Render("[ ]")
	if m.switchPreserveSkills {
		pCheck = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("[✓]")
	}
	pDesc := fmt.Sprintf("  %s Preserve Active Skills Profile", pCheck)
	if m.switchPreserveSkills {
		pDesc = fmt.Sprintf("  %s %s", pCheck, lipgloss.NewStyle().Bold(true).Foreground(cyanColor).Render("Preserve Active Skills Profile"))
	}
	lines = append(lines, fmt.Sprintf("%-42s %s", pDesc, lipgloss.NewStyle().Foreground(subtleColor).Render("[p to toggle]")))

	lines = append(lines, divider)
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  [1-5 / ↑/↓] Select Target  •  [m] Mode  •  [h/p] Options  •  [Enter] Confirm  •  [Esc] Cancel"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderAccountDialog(width, height int) string {
	bStyle := activeBorder

	var lines []string
	sessTitle := "Unknown"
	currTool := "agy"
	currAccount := "default"
	if m.accountTargetInst != nil {
		sessTitle = m.accountTargetInst.Title
		currTool = m.accountTargetInst.Tool
		if m.accountTargetInst.Account != "" {
			currAccount = m.accountTargetInst.Account
		}
	}

	divWidth := width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth))

	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("⚙ Switch Account for \"" + sessTitle + "\"")
	lines = append(lines, header)

	subInfo := fmt.Sprintf("Harness: %s   •   Current: %s", RenderToolBadge(currTool), lipgloss.NewStyle().Foreground(successColor).Bold(true).Render(currAccount))
	lines = append(lines, subInfo, m.accountProfileSummary())
	lines = append(lines, divider)

	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(primaryColor).Render("Select Target Account:"))

	for i, acc := range m.accounts {
		isTarget := i == m.accountModalIdx
		isCurrent := strings.EqualFold(acc.Email, currAccount) || strings.EqualFold(acc.ID, currAccount)

		badge := ""
		if isCurrent {
			badge = " " + lipgloss.NewStyle().Foreground(warningColor).Render("[Current]")
		} else if isTarget {
			badge = " " + lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("[Selected]")
		}

		accName := acc.Email
		if accName == "" {
			accName = acc.ID
		}

		bar := m.renderProgressBar(acc.RemainingQuotaPct, 10)
		quotaStr := fmt.Sprintf("%s %4.0f%%", bar, acc.RemainingQuotaPct)

		if isTarget {
			rowStr := fmt.Sprintf("▶ [%d] %-32s %s%s", i+1, truncateString(accName, 32), quotaStr, badge)
			lines = append(lines, lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("#ffffff")).
				Background(bgHighlight).
				Padding(0, 1).
				Width(divWidth).
				Render(rowStr))
		} else {
			rowStr := fmt.Sprintf("  [%d] %-32s %s%s", i+1, truncateString(accName, 32), quotaStr, badge)
			lines = append(lines, lipgloss.NewStyle().
				Foreground(lipgloss.Color("#a9b1d6")).
				Padding(0, 1).
				Render(rowStr))
		}
	}

	lines = append(lines, divider)

	// Respawn toggle
	rCheck := lipgloss.NewStyle().Foreground(subtleColor).Render("[ ]")
	if m.accountModalRespawn {
		rCheck = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[✓]")
	}
	rDesc := fmt.Sprintf("  %s Seamlessly respawn tmux pane with new credentials", rCheck)
	if m.accountModalRespawn {
		rDesc = fmt.Sprintf("  %s %s", rCheck, lipgloss.NewStyle().Bold(true).Foreground(successColor).Render("Seamlessly respawn tmux pane with new credentials"))
	}
	lines = append(lines, fmt.Sprintf("%-42s %s", rDesc, lipgloss.NewStyle().Foreground(subtleColor).Render("[r to toggle]")))

	lines = append(lines, divider)
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  [1-9 / ↑/↓] Select Account  •  [Enter] Confirm  •  [Esc] Cancel"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderNewTaskDialog(width, height int) string {
	bStyle := activeBorder

	var lines []string
	lines = append(lines, "Account profile: "+m.newTaskAccountProfile+" [Ctrl+G to cycle; blank = global default]")
	divWidth := width - 4
	if divWidth < 10 {
		divWidth = 10
	}
	divider := lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth))

	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("✨ New Task (Agent-Desk Provisioning & Hotkeys)")
	lines = append(lines, header)
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Isolated sandbox, credentials, live autocompletion, and hotkeys"))
	lines = append(lines, divider)

	// Helper for cursor-aware text input rendering
	renderInput := func(fieldIdx int, label string, ti *TextInputState, placeholder string) string {
		isFocused := m.newTaskFieldFocus == fieldIdx
		prefix := "  "
		lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
		displayVal := ti.Value
		if displayVal == "" && !isFocused {
			displayVal = lipgloss.NewStyle().Foreground(subtleColor).Render(placeholder)
		} else if isFocused {
			prefix = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("▶ ")
			lblStyle = lipgloss.NewStyle().Foreground(cyanColor).Bold(true)
			runes := []rune(ti.Value)
			cur := ti.Cursor
			if cur < 0 {
				cur = 0
			}
			if cur > len(runes) {
				cur = len(runes)
			}
			before := string(runes[:cur])
			cursorChar := " "
			after := ""
			if cur < len(runes) {
				cursorChar = string(runes[cur])
				if cur+1 < len(runes) {
					after = string(runes[cur+1:])
				}
			}
			renderedCursor := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(cyanColor).Render(cursorChar)
			displayVal = before + renderedCursor + after
		} else {
			displayVal = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Render(ti.Value)
		}
		return fmt.Sprintf("%s%-14s %s", prefix, lblStyle.Render(label+":"), displayVal)
	}

	// 0. Title
	lines = append(lines, renderInput(0, "Task Title", &m.newTaskTitleInput, "e.g. revamp-auth-service"))

	// 1. Path
	pathLine := renderInput(1, "Project Path", &m.newTaskPathInput, "e.g. /home/user/myproject (Tab: auto-complete)")
	expandedPath := m.newTaskPathInput.Value
	if strings.HasPrefix(expandedPath, "~") {
		home, _ := os.UserHomeDir()
		expandedPath = filepath.Join(home, strings.TrimPrefix(expandedPath, "~"))
	}
	if fi, err := os.Stat(expandedPath); err == nil && fi.IsDir() {
		pathLine += " " + lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("[✓ exists]")
	} else if strings.TrimSpace(m.newTaskPathInput.Value) != "" {
		pathLine += " " + lipgloss.NewStyle().Foreground(warningColor).Render("[+ new dir]")
	}
	lines = append(lines, pathLine)

	// Path Suggestions Bar
	if m.newTaskPathCycler.IsActive() {
		matches := m.newTaskPathCycler.Matches()
		curIdx := m.newTaskPathCycler.CurrentIndex()
		var chips []string
		for i, match := range matches {
			if i >= 4 {
				chips = append(chips, lipgloss.NewStyle().Foreground(subtleColor).Render(fmt.Sprintf("+%d more", len(matches)-4)))
				break
			}
			chipStr := match
			if len(chipStr) > 22 {
				chipStr = "..." + chipStr[len(chipStr)-19:]
			}
			if i == curIdx {
				chips = append(chips, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(accentColor).Padding(0, 1).Render(chipStr))
			} else {
				chips = append(chips, lipgloss.NewStyle().Foreground(subtleColor).Render(chipStr))
			}
		}
		suggestHeader := lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render(fmt.Sprintf("   ↳ Suggestions [%d/%d]: ", curIdx+1, len(matches)))
		lines = append(lines, suggestHeader+strings.Join(chips, "  "))
	}

	// 2. Group
	groupLine := renderInput(2, "Group Path", &m.newTaskGroupInput, "e.g. Work/Backend (Tab: auto-complete)")
	lines = append(lines, groupLine)

	// Group Suggestions Bar
	if m.newTaskGroupCycler.IsActive() {
		matches := m.newTaskGroupCycler.Matches()
		curIdx := m.newTaskGroupCycler.CurrentIndex()
		var chips []string
		for i, match := range matches {
			if i >= 4 {
				chips = append(chips, lipgloss.NewStyle().Foreground(subtleColor).Render(fmt.Sprintf("+%d more", len(matches)-4)))
				break
			}
			if i == curIdx {
				chips = append(chips, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(accentColor).Padding(0, 1).Render(match))
			} else {
				chips = append(chips, lipgloss.NewStyle().Foreground(subtleColor).Render(match))
			}
		}
		suggestHeader := lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render(fmt.Sprintf("   ↳ Groups [%d/%d]: ", curIdx+1, len(matches)))
		lines = append(lines, suggestHeader+strings.Join(chips, "  "))
	}

	lines = append(lines, divider)

	// 3. Harness Selector
	{
		isFocused := m.newTaskFieldFocus == 3
		prefix := "  "
		lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
		if isFocused {
			prefix = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("▶ ")
			lblStyle = lipgloss.NewStyle().Foreground(cyanColor).Bold(true)
		}

		var toolBadges []string
		for idx, h := range availableHarnesses {
			chipLabel := fmt.Sprintf("[%d] %s", idx+1, h.ID)
			if idx == m.newTaskToolIdx {
				toolBadges = append(toolBadges, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(cyanColor).Padding(0, 1).Render(chipLabel))
			} else {
				toolBadges = append(toolBadges, lipgloss.NewStyle().Foreground(subtleColor).Render(chipLabel))
			}
		}
		cycleHint := ""
		if isFocused {
			cycleHint = " " + lipgloss.NewStyle().Foreground(subtleColor).Render("(←/→/Space/1-5)")
		}
		lines = append(lines, fmt.Sprintf("%s%-14s %s%s", prefix, lblStyle.Render("Harness:"), strings.Join(toolBadges, "  "), cycleHint))
	}

	// 4. Account Selector
	{
		isFocused := m.newTaskFieldFocus == 4
		prefix := "  "
		lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
		if isFocused {
			prefix = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("▶ ")
			lblStyle = lipgloss.NewStyle().Foreground(cyanColor).Bold(true)
		}

		accName := "default"
		quotaStr := ""
		if len(m.newTaskAccounts()) > 0 && m.newTaskAccountIdx >= 0 && m.newTaskAccountIdx < len(m.newTaskAccounts()) {
			acc := m.newTaskAccounts()[m.newTaskAccountIdx]
			accName = acc.Email
			if accName == "" {
				accName = acc.ID
			}
			bar := m.renderProgressBar(acc.RemainingQuotaPct, 8)
			quotaStr = fmt.Sprintf(" %s %3.0f%%", bar, acc.RemainingQuotaPct)
		}
		var accDisplay string
		if isFocused {
			accDisplay = lipgloss.NewStyle().Bold(true).Foreground(successColor).Render(fmt.Sprintf("[%d/%d] %s%s", m.newTaskAccountIdx+1, len(m.newTaskAccounts()), accName, quotaStr)) +
				" " + lipgloss.NewStyle().Foreground(subtleColor).Render("(←/→/Space/1-9)")
		} else {
			accDisplay = lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6")).Render(accName + quotaStr)
		}
		lines = append(lines, fmt.Sprintf("%s%-14s %s", prefix, lblStyle.Render("Account:"), accDisplay))
	}

	// 5. Skill Profile Multi-Chip Selector
	{
		isFocused := m.newTaskFieldFocus == 5
		prefix := "  "
		lblStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#a9b1d6"))
		if isFocused {
			prefix = lipgloss.NewStyle().Foreground(cyanColor).Bold(true).Render("▶ ")
			lblStyle = lipgloss.NewStyle().Foreground(cyanColor).Bold(true)
		}

		if m.newTaskSkillProfiles == nil {
			m.newTaskSkillProfiles = map[string]bool{"core": true}
		}

		var profBadges []string
		for idx, p := range m.skillProfileOptions() {
			isSelected := m.newTaskSkillProfiles[p]
			isUnderCursor := isFocused && idx == m.newTaskSkillCursor

			checkMark := "[ ]"
			if isSelected {
				checkMark = "[✓]"
			}

			chipText := fmt.Sprintf("[%d] %s %s", idx+1, checkMark, p)
			if isUnderCursor {
				profBadges = append(profBadges, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#1a1b26")).Background(accentColor).Padding(0, 1).Render(chipText))
			} else if isSelected {
				profBadges = append(profBadges, lipgloss.NewStyle().Bold(true).Foreground(successColor).Render(chipText))
			} else {
				profBadges = append(profBadges, lipgloss.NewStyle().Foreground(subtleColor).Render(chipText))
			}
		}
		cycleHint := ""
		if isFocused {
			cycleHint = " " + lipgloss.NewStyle().Foreground(subtleColor).Render("(←/→/Space/1-6)")
		}
		lines = append(lines, fmt.Sprintf("%s%-14s %s%s", prefix, lblStyle.Render("Skill Profiles:"), strings.Join(profBadges, " "), cycleHint))
	}

	lines = append(lines, divider)

	// 6. Initial Prompt (Optional)
	lines = append(lines, renderInput(6, "Initial Prompt", &m.newTaskPromptInput, "(optional startup instruction sent to tmux session)"))

	lines = append(lines, divider)
	lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("  [Tab] Autocomplete/Next  •  [Ctrl+S] Create Task  •  [Ctrl+R] Recent Paths  •  [Ctrl+W] Rubout  •  [Esc] Cancel"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderSkillsOverlay(width, height int) string { return m.skillsView(width, height) }

func (m Model) renderProgressBar(pct float64, width int) string {
	filled := int((pct / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	empty := width - filled

	bar := strings.Repeat("█", filled) + strings.Repeat("░", empty)
	color := successColor
	if pct < 20.0 {
		color = errorColor
	} else if pct < 50.0 {
		color = warningColor
	}
	return lipgloss.NewStyle().Foreground(color).Render(bar)
}

func (m Model) renderCommandPalette(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(cyanColor).
		Padding(1, 2)

	var lines []string
	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("⚡ Global Command Palette")
	sub := lipgloss.NewStyle().Foreground(subtleColor).Render("Quick Access to All AI-Desk Operations")
	lines = append(lines, header+"   •   "+sub)

	divWidth := width - 8
	if divWidth < 10 {
		divWidth = 10
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth)))

	availLines := height - 6
	if availLines < 5 {
		availLines = 5
	}
	startIdx := 0
	if m.commandPaletteIdx >= availLines {
		startIdx = m.commandPaletteIdx - availLines + 1
	}
	endIdx := startIdx + availLines
	if endIdx > len(commandPaletteItems) {
		endIdx = len(commandPaletteItems)
	}

	for i := startIdx; i < endIdx; i++ {
		item := commandPaletteItems[i]
		keyBadge := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#1a1b26")).
			Background(cyanColor).
			Padding(0, 1).
			Render("[" + item.Key + "]")
		title := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render(item.Title)
		descMax := width - len(item.Key) - len(item.Title) - 24
		if descMax < 10 {
			descMax = 10
		}
		desc := lipgloss.NewStyle().Foreground(subtleColor).Render(truncateString(item.Description, descMax))

		row := fmt.Sprintf("%-10s  %-28s  %s", keyBadge, title, desc)
		if i == m.commandPaletteIdx {
			lines = append(lines, selectedRow.Width(width-4).Render("▎ "+row))
		} else {
			lines = append(lines, normalRow.Render("  "+row))
		}
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("Navigate: [↑/↓/k/j]   •   Execute: [Enter]   •   Close: [Esc/?]"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderRoutinesDialog(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#bb9af7")).
		Padding(1, 2)

	var lines []string
	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("⏱ AI-Desk Scheduled Routines & Background Cron")
	countBadge := lipgloss.NewStyle().Foreground(subtleColor).Render(fmt.Sprintf("(%d registered)", len(m.routinesList)))
	lines = append(lines, header+" "+countBadge)

	divWidth := width - 8
	if divWidth < 10 {
		divWidth = 10
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth)))

	if len(m.routinesList) == 0 {
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("No recurring routines registered yet."))
		lines = append(lines, lipgloss.NewStyle().Foreground(subtleColor).Render("Add one via CLI: ai-desk routine add --title \"Daily Review\" --cron \"0 9 * * *\" --command \"agy task run triage\""))
		lines = append(lines, "")
		lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("[Esc] Close Panel"))
		return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
	}

	availItems := (height - 6) / 2
	if availItems < 2 {
		availItems = 2
	}
	startIdx := 0
	if m.routinesSelectedIdx >= availItems {
		startIdx = m.routinesSelectedIdx - availItems + 1
	}
	endIdx := startIdx + availItems
	if endIdx > len(m.routinesList) {
		endIdx = len(m.routinesList)
	}

	for i := startIdx; i < endIdx; i++ {
		r := m.routinesList[i]
		statusBullet := "●"
		statusColor := successColor
		if r.Status == "paused" {
			statusBullet = "⏸"
			statusColor = warningColor
		}

		statusBadge := lipgloss.NewStyle().Foreground(statusColor).Bold(true).Render(statusBullet + " " + strings.ToUpper(r.Status))
		schedBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("#e0af68")).Render(r.CronExpr)
		toolBadge := RenderToolBadge(r.Tool)

		row1 := fmt.Sprintf("%-28s %s  %s  %s", truncateString(r.Title, 26), schedBadge, toolBadge, statusBadge)
		nextStr := "never"
		if r.NextRunAt > 0 {
			nextStr = time.Unix(r.NextRunAt, 0).Format("Jan 02 15:04")
		}
		cmdMax := width - 36
		if cmdMax < 10 {
			cmdMax = 10
		}
		row2 := fmt.Sprintf("    ↳ Command: %s  |  Next: %s", lipgloss.NewStyle().Foreground(cyanColor).Render(truncateString(r.Command, cmdMax)), nextStr)

		if i == m.routinesSelectedIdx {
			lines = append(lines, selectedRow.Width(width-4).Render("▎ "+row1))
			lines = append(lines, selectedRow.Width(width-4).Render("  "+row2))
		} else {
			lines = append(lines, normalRow.Render("  "+row1))
			lines = append(lines, normalRow.Render("  "+row2))
		}
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(warningColor).Render("Controls: [Enter/x] Run Now   •   [p] Pause/Resume   •   [d] Delete   •   [Esc] Close"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderVoiceDialog(width, height int) string {
	bStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("#7aa2f7")).
		Padding(1, 2)

	var lines []string
	header := lipgloss.NewStyle().Bold(true).Foreground(accentColor).Render("🎙 AI-Desk Resident Voice & Virtual Assistant")
	sub := lipgloss.NewStyle().Foreground(subtleColor).Render("Grok-Bot Mode & Neural Speech Copilot")
	lines = append(lines, header+"   •   "+sub)

	divWidth := width - 8
	if divWidth < 10 {
		divWidth = 10
	}
	lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#2f3549")).Render(strings.Repeat("─", divWidth)))

	// Telemetry & Pairing
	st := m.voiceStatusCache
	if st == nil {
		st, _ = voice.GetStatus()
		m.voiceStatusCache = st
	}

	ttsModel := "Kyutai Pocket-TTS (24kHz Mimi)"
	asrModel := "Faster-Whisper (small.en INT8)"
	daemonState := lipgloss.NewStyle().Foreground(warningColor).Render("Standby (Fast Mode)")
	if st != nil && st.DaemonActive {
		daemonState = lipgloss.NewStyle().Foreground(successColor).Bold(true).Render("● Active Resident Daemon")
	}

	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("◆ SUBSYSTEM STATUS"))
	lines = append(lines, fmt.Sprintf("  • TTS Engine:       %s", lipgloss.NewStyle().Foreground(cyanColor).Render(ttsModel)))
	lines = append(lines, fmt.Sprintf("  • ASR Model:        %s", lipgloss.NewStyle().Foreground(cyanColor).Render(asrModel)))
	lines = append(lines, fmt.Sprintf("  • Voice Daemon:     %s", daemonState))

	sessTitle := "Standby Voice Terminal"
	sessTool := "Generic"
	if len(m.treeItems) > 0 && m.selectedTreeIdx >= 0 && m.selectedTreeIdx < len(m.treeItems) {
		item := m.treeItems[m.selectedTreeIdx]
		if !item.IsGroup && item.Session != nil {
			sessTitle = item.Session.Title
			sessTool = item.Session.Tool
		}
	}
	lines = append(lines, fmt.Sprintf("  • Active Target:    %s [%s]", lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e0af68")).Render(sessTitle), sessTool))
	lines = append(lines, "")

	lines = append(lines, lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#ffffff")).Render("◆ QUICK ACTIONS"))
	actions := []string{
		"[1] / [Enter]  Interactive Voice Chat with Active Session",
		"[2]            Launch Standby Fast Voice Terminal",
		"[3]            Speak Situational Status Briefing (Grok-Bot summary)",
		"[4]            Toggle Resident Voice Daemon",
		"[5]            Run 10-Second SaaS Tech Stack Reverse Engineer",
	}
	for _, act := range actions {
		lines = append(lines, lipgloss.NewStyle().Foreground(lipgloss.Color("#7dcfff")).Render("  "+act))
	}

	lines = append(lines, "")
	lines = append(lines, lipgloss.NewStyle().Foreground(warningColor).Render("Select action [1-5]   •   Close panel [Esc/q]"))

	return bStyle.Width(width).Height(height).Render(strings.Join(lines, "\n"))
}

func (m Model) renderFooter() string {
	var keys []string

	formatKey := func(k, desc string) string {
		return helpKeyStyle.Render(k) + " " + helpDescStyle.Render(desc)
	}

	if m.showInspector {
		keys = []string{
			formatKey("Esc", "Close Inspector"),
			formatKey("Enter", "Attach Tmux"),
			formatKey("s", "Skills"),
			formatKey("q", "Quit"),
		}
	} else if m.showDeleteDialog {
		keys = []string{
			formatKey("y", "Confirm Cascade Delete"),
			formatKey("n/Esc", "Cancel"),
		}
	} else if m.showSwitchDialog {
		keys = []string{
			formatKey("1-5", "Select Target"),
			formatKey("m/f/Tab", "Toggle Mode"),
			formatKey("h", "Toggle Handoff"),
			formatKey("p", "Toggle Skills"),
			formatKey("Enter", "Confirm Switch"),
			formatKey("Esc", "Cancel"),
		}
	} else if m.showAccountDialog {
		keys = []string{
			formatKey("1-9", "Select Account"),
			formatKey("r", "Toggle Respawn"),
			formatKey("Enter", "Confirm Account"),
			formatKey("Esc", "Cancel"),
		}
	} else if m.showNewTaskDialog {
		keys = []string{
			formatKey("Tab/Shift+Tab", "Next/Prev Field"),
			formatKey("←/→/Space", "Cycle Option"),
			formatKey("Enter", "Create Task"),
			formatKey("Esc", "Cancel"),
		}
	} else if m.showSkillsOverlay {
		keys = []string{
			formatKey("s", "Close Skills"),
			formatKey("↑/↓", "Navigate Profiles"),
			formatKey("Enter", "Select Default"),
			formatKey("q", "Quit"),
		}
	} else if m.showCommandPalette {
		keys = []string{
			formatKey("↑/↓", "Navigate Commands"),
			formatKey("Enter", "Execute"),
			formatKey("Esc/?", "Close Palette"),
		}
	} else if m.showRoutinesDialog {
		keys = []string{
			formatKey("↑/↓", "Navigate"),
			formatKey("Enter/x", "Run Now"),
			formatKey("p", "Pause/Resume"),
			formatKey("d", "Delete"),
			formatKey("Esc", "Close"),
		}
	} else if m.showVoiceDialog {
		keys = []string{
			formatKey("1/Enter", "Voice Chat"),
			formatKey("2", "Standby"),
			formatKey("3", "Briefing"),
			formatKey("4", "Daemon"),
			formatKey("5", "SaaS Stack"),
			formatKey("Esc", "Close"),
		}
	} else {
		keys = []string{
			formatKey("Tab", "Switch Pane"),
			formatKey("↑/↓", "Navigate"),
			formatKey("←/→", "Collapse/Expand"),
			formatKey("Enter", "Attach"),
			formatKey("?", "Palette"),
			formatKey("r", "Routines"),
			formatKey("v", "Voice Chat"),
			formatKey("n", "New Task"),
			formatKey("w", "Harness"),
			formatKey("a", "Account"),
			formatKey("f", "Failover"),
			formatKey("d", "Delete"),
			formatKey("s", "Skills"),
			formatKey("i", "Inspect"),
			formatKey("R", "Refresh"),
			formatKey("q", "Quit"),
		}
	}
	return lipgloss.NewStyle().MarginTop(1).Render(strings.Join(keys, "   •   "))
}

func truncateString(s string, maxLen int) string {
	if maxLen <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return string(runes[:maxLen])
	}
	return string(runes[:maxLen-3]) + "..."
}

func truncateVisibleLine(s string, maxCols int) string {
	if maxCols <= 0 {
		return ""
	}
	var sb strings.Builder
	col := 0
	inEscape := false

	runes := []rune(s)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		if r == '\x1b' {
			inEscape = true
			sb.WriteRune(r)
			continue
		}
		if inEscape {
			sb.WriteRune(r)
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '~' {
				inEscape = false
			}
			continue
		}

		w := 1
		if r > 127 {
			w = runewidth.RuneWidth(r)
		}
		if col+w > maxCols-3 && i < len(runes)-1 {
			sb.WriteString("...")
			break
		}
		sb.WriteRune(r)
		col += w
		if col >= maxCols {
			break
		}
	}
	sb.WriteString("\x1b[0m")
	return sb.String()
}
