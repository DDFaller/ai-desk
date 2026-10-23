package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/rotation"
)

func TestUI_ThreePaneModelLifecycle(t *testing.T) {
	m := NewModel()

	// 1. Send WindowSizeMsg
	mUpdated, _ := m.Update(tea.WindowSizeMsg{Width: 140, Height: 35})
	model := mUpdated.(Model)

	// 2. Render 3-Pane View
	view := model.View()
	if !strings.Contains(view, "AI DESK") {
		t.Errorf("expected view to contain 'AI DESK', got:\n%s", view)
	}
	if !strings.Contains(view, "v"+Version) {
		t.Errorf("expected view to contain 'v%s', got:\n%s", Version, view)
	}
	// Check all 3 panes are present in the view
	if !strings.Contains(strings.ToUpper(view), "SELECTION") {
		t.Errorf("expected view to contain Pane 1 'Selection', got:\n%s", view)
	}
	if !strings.Contains(strings.ToUpper(view), "TASK") {
		t.Errorf("expected view to contain Pane 2 'Task', got:\n%s", view)
	}
	if !strings.Contains(strings.ToUpper(view), "SESSION STATUS") {
		t.Errorf("expected view to contain Pane 3 'Session Status', got:\n%s", view)
	}

	// 3. Test Tab cycling through the 3 Panes
	if model.focus != FocusSelection {
		t.Errorf("expected initial focus FocusSelection, got %v", model.focus)
	}

	// Tab -> FocusTask
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.focus != FocusTask {
		t.Errorf("expected focus FocusTask after Tab, got %v", model.focus)
	}

	// Tab -> FocusStatus
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.focus != FocusStatus {
		t.Errorf("expected focus FocusStatus after 2nd Tab, got %v", model.focus)
	}

	// Tab -> FocusSelection
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.focus != FocusSelection {
		t.Errorf("expected focus FocusSelection after 3rd Tab, got %v", model.focus)
	}

	// 4. Test Delete dialog open and cancel
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	model = mUpdated.(Model)
	if len(model.treeItems) > 0 {
		if !model.showDeleteDialog {
			t.Errorf("expected delete dialog open on 'd'")
		}
		// Cancel dialog with esc
		mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = mUpdated.(Model)
		if model.showDeleteDialog {
			t.Errorf("expected delete dialog closed after Escape")
		}
	}

	// 5. Test Switch Agent dialog on 'w'
	model.showDeleteDialog = false
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	model = mUpdated.(Model)
	if len(model.treeItems) > 0 && !model.treeItems[model.selectedTreeIdx].IsGroup {
		if !model.showSwitchDialog {
			t.Errorf("expected switch dialog open on 'w'")
		}
		// Test toggle handoff 'h'
		initialHandoff := model.switchWithHandoff
		mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
		model = mUpdated.(Model)
		if model.switchWithHandoff == initialHandoff {
			t.Errorf("expected handoff toggled on 'h'")
		}

		// Test target harness selection '2'
		mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
		model = mUpdated.(Model)
		if model.switchTargetToolIdx != 1 {
			t.Errorf("expected target tool idx 1 on '2', got %d", model.switchTargetToolIdx)
		}

		// Verify dialog view renders
		dialogView := model.View()
		if !strings.Contains(strings.ToUpper(dialogView), "SWITCH AGENT") {
			t.Errorf("expected view to contain 'SWITCH AGENT', got:\n%s", dialogView)
		}

		// Cancel switch dialog with esc
		mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
		model = mUpdated.(Model)
		if model.showSwitchDialog {
			t.Errorf("expected switch dialog closed after Escape")
		}
	}

	// 6. Test Quit key
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected quit command on 'q'")
	}
}

func TestUI_RenderToolBadges(t *testing.T) {
	tools := []string{"agy", "claude", "codex", "opencode"}
	for _, tool := range tools {
		badge := RenderToolBadge(tool)
		if !strings.Contains(badge, tool) {
			t.Errorf("expected badge for %q to contain %q, got %q", tool, tool, badge)
		}
	}
}

func TestUI_RenderStatusPaneCards(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 30

	// Empty group/analytics view
	out := m.renderStatusPane(38, 25)
	if !strings.Contains(out, "GROUP ANALYTICS") && !strings.Contains(out, "SESSION STATUS") {
		t.Errorf("expected status pane to contain headers, got:\n%s", out)
	}

	// With inspection
	m.currentInspection = &deck.SessionInspection{
		Instance: deck.InstanceRecord{
			ID:        "test-sess-123",
			Title:     "Feature Auth",
			Tool:      "codex",
			GroupPath: "features/auth",
		},
		ActiveProfile: "core",
		ActiveSkills:  []string{"verification", "git"},
		HasTokenMetrics: true,
		TokensUsed: deck.TokenUsage{
			TotalTokens: 15420,
			InputTokens: 12000,
			OutputTokens: 3420,
		},
	}
	out = m.renderStatusPane(38, 25)
	if !strings.Contains(out, "SESSION IDENTITY") {
		t.Errorf("expected status pane to contain 'SESSION IDENTITY', got:\n%s", out)
	}
	if !strings.Contains(out, "SKILLS & PROMPT TAX") {
		t.Errorf("expected status pane to contain 'SKILLS & PROMPT TAX', got:\n%s", out)
	}
	if !strings.Contains(out, "TOKEN TELEMETRY") {
		t.Errorf("expected status pane to contain 'TOKEN TELEMETRY', got:\n%s", out)
	}
}

func TestUI_TokenExhaustionDisplay(t *testing.T) {
	m := NewModel()
	m.currentInspection = &deck.SessionInspection{
		Instance: deck.InstanceRecord{
			ID:          "sess-exhausted",
			Title:       "Codex Task",
			Tool:        "codex",
			Status:      "running",
			TmuxSession: "agentdeck_codex_429",
		},
		HasTokenMetrics: true,
		TokensUsed: deck.TokenUsage{
			TotalTokens:        856579,
			InputTokens:        836814,
			OutputTokens:       19765,
			RateLimitUsedPct:   99.0,
			RateLimitResetAt:   time.Now().Add(5 * 24 * time.Hour),
			Exhausted:          true,
			ExhaustionReason:   "usage_limit_exceeded",
			PlanType:           "prolite",
			ModelContextWindow: 258400,
		},
	}

	out := m.renderStatusPane(50, 30)
	if !strings.Contains(out, "QUOTA EXHAUSTED") {
		t.Errorf("expected status pane to display 'QUOTA EXHAUSTED', got:\n%s", out)
	}
	if !strings.Contains(out, "usage_limit_exceeded") {
		t.Errorf("expected status pane to display 'usage_limit_exceeded', got:\n%s", out)
	}
	if !strings.Contains(out, "99.0% used") {
		t.Errorf("expected status pane to display '99.0%%%% used', got:\n%s", out)
	}
	if !strings.Contains(out, "switch session harness") {
		t.Errorf("expected switch to AGY hint in status pane, got:\n%s", out)
	}
	if !strings.Contains(out, "EXHAUSTED") {
		t.Errorf("expected voice button to indicate exhausted state, got:\n%s", out)
	}
}

func TestUI_RenderSwitchDialogModal(t *testing.T) {
	m := NewModel()
	m.switchTargetInst = &deck.InstanceRecord{
		ID:        "sess-switch-test",
		Title:     "Switch Worker",
		Tool:      "codex",
		GroupPath: "workers/etl",
	}
	m.showSwitchDialog = true
	m.switchTargetToolIdx = 0
	m.switchWithHandoff = true
	m.switchPreserveSkills = true

	out := m.renderSwitchDialog(60, 20)
	if !strings.Contains(out, "Switch Agent for") {
		t.Errorf("expected switch dialog header, got:\n%s", out)
	}
	if !strings.Contains(out, "Google Antigravity (agy)") {
		t.Errorf("expected agy option, got:\n%s", out)
	}
	if !strings.Contains(out, "Anthropic Claude Code (claude)") {
		t.Errorf("expected claude option, got:\n%s", out)
	}
	if !strings.Contains(out, "Synthesize Context Handoff Brief") {
		t.Errorf("expected handoff toggle, got:\n%s", out)
	}
}

func TestUI_RenderAccountDialogModal(t *testing.T) {
	m := NewModel()
	m.accountTargetInst = &deck.InstanceRecord{
		ID:        "sess-acc-test",
		Title:     "Account Worker",
		Tool:      "agy",
		Account:   "danielmcf15@gmail.com",
		GroupPath: "workers/etl",
	}
	m.accounts = []rotation.AccountStatus{
		{ID: "danielmcf15@gmail.com", Email: "danielmcf15@gmail.com", RemainingQuotaPct: 100.0},
		{ID: "clownfishexperience@gmail.com", Email: "clownfishexperience@gmail.com", RemainingQuotaPct: 68.8},
	}
	m.showAccountDialog = true
	m.accountModalIdx = 1
	m.accountModalRespawn = true

	out := m.renderAccountDialog(70, 20)
	if !strings.Contains(out, "Switch Account for") {
		t.Errorf("expected account dialog header, got:\n%s", out)
	}
	if !strings.Contains(out, "danielmcf15@gmail.com") {
		t.Errorf("expected first account, got:\n%s", out)
	}
	if !strings.Contains(out, "clownfishexperience@gmail.com") {
		t.Errorf("expected second account, got:\n%s", out)
	}
	if !strings.Contains(out, "Seamlessly respawn tmux pane") {
		t.Errorf("expected respawn toggle, got:\n%s", out)
	}
}

func TestUI_SelectionViewportWindowing(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 30

	// Populate tree with 100 fake items
	var items []deck.TreeItem
	for i := 0; i < 100; i++ {
		items = append(items, deck.TreeItem{
			IsGroup: false,
			Session: &deck.InstanceRecord{
				ID:        fmt.Sprintf("task-%d", i),
				Title:     fmt.Sprintf("Worker Task #%d", i),
				Tool:      "agy",
				Status:    "running",
				GroupPath: "general",
			},
			Depth: 1,
		})
	}
	m.treeItems = items
	m.selectedTreeIdx = 0

	// Render selection pane with height 20 lines
	rendered := m.renderSelectionPane(35, 20)
	if !strings.Contains(rendered, "Worker Task #0") {
		t.Errorf("expected first task to be visible")
	}
	if !strings.Contains(rendered, "more below") {
		t.Errorf("expected bottom scroll indicator 'more below', got:\n%s", rendered)
	}

	// Move cursor down to item 50
	m.selectedTreeIdx = 50
	renderedDown := m.renderSelectionPane(35, 20)
	if !strings.Contains(renderedDown, "Worker Task #50") {
		t.Errorf("expected task #50 to be visible when scrolled")
	}
	if !strings.Contains(renderedDown, "more above") {
		t.Errorf("expected top scroll indicator 'more above', got:\n%s", renderedDown)
	}
}

func TestUI_NewTaskModal(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 35

	// 1. Press 'n' to open New Task modal
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model := mUpdated.(Model)

	if !model.showNewTaskDialog {
		t.Fatalf("expected showNewTaskDialog true on 'n'")
	}
	if model.newTaskFieldFocus != 0 {
		t.Errorf("expected focus on field 0 (Title), got %d", model.newTaskFieldFocus)
	}

	// 2. Type "Auth Service" into Title
	for _, r := range "Auth Service" {
		mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		model = mUpdated.(Model)
	}
	if model.newTaskTitle != "Auth Service" {
		t.Errorf("expected newTaskTitle 'Auth Service', got %q", model.newTaskTitle)
	}

	// 3. Tab to field 1 (Path)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.newTaskFieldFocus != 1 {
		t.Errorf("expected focus on field 1 (Path), got %d", model.newTaskFieldFocus)
	}

	// 4. Tab to field 3 (Harness)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model) // field 2 (Group)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model) // field 3 (Harness)
	if model.newTaskFieldFocus != 3 {
		t.Errorf("expected focus on field 3 (Harness), got %d", model.newTaskFieldFocus)
	}

	// Cycle harness with 'space'
	origToolIdx := model.newTaskToolIdx
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeySpace})
	model = mUpdated.(Model)
	if model.newTaskToolIdx == origToolIdx {
		t.Errorf("expected tool idx changed after Space")
	}

	// 5. Render dialog
	dialog := model.renderNewTaskDialog(60, 25)
	if !strings.Contains(dialog, "New Task") {
		t.Errorf("expected dialog to contain 'New Task', got:\n%s", dialog)
	}
	if !strings.Contains(dialog, "Auth Service") {
		t.Errorf("expected dialog to contain typed title 'Auth Service', got:\n%s", dialog)
	}

	// 6. Cancel dialog with Esc
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEscape})
	model = mUpdated.(Model)
	if model.showNewTaskDialog {
		t.Errorf("expected showNewTaskDialog false after Esc")
	}
}

func TestUI_NewTaskModal_MultiSkillsAndVoxStudio(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 35

	// 1. Open New Task modal
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model := mUpdated.(Model)

	if !model.showNewTaskDialog {
		t.Fatalf("expected showNewTaskDialog true on 'n'")
	}

	// 2. Tab to Field 3 (Harness)
	model.newTaskFieldFocus = 3
	// Press '5' for vox-studio (5th option)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	model = mUpdated.(Model)

	if availableHarnesses[model.newTaskToolIdx].ID != "vox-studio" {
		t.Errorf("expected harness vox-studio on '5', got %s", availableHarnesses[model.newTaskToolIdx].ID)
	}
	if model.newTaskGroup != "Creative/VideoStudio" {
		t.Errorf("expected group 'Creative/VideoStudio' for vox-studio, got %q", model.newTaskGroup)
	}
	if !model.newTaskSkillProfiles["video"] {
		t.Errorf("expected 'video' skill profile to be enabled for vox-studio")
	}

	// 3. Tab to Field 5 (Skill Profiles)
	model.newTaskFieldFocus = 5
	// Press '2' to toggle 'web' profile
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	model = mUpdated.(Model)

	if !model.newTaskSkillProfiles["web"] {
		t.Errorf("expected 'web' skill profile to be toggled on")
	}
	if !model.newTaskSkillProfiles["core"] {
		t.Errorf("expected 'core' skill profile to still be active")
	}

	// 4. Render dialog and verify UI contains multi-chip indicators
	rendered := model.renderNewTaskDialog(80, 25)
	if !strings.Contains(rendered, "[✓] core") {
		t.Errorf("expected rendered dialog to contain '[✓] core', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "[✓] web") {
		t.Errorf("expected rendered dialog to contain '[✓] web', got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "vox-studio") {
		t.Errorf("expected rendered dialog to contain 'vox-studio', got:\n%s", rendered)
	}
}

func TestUI_ResidentVoiceStatusAndAction(t *testing.T) {
	m := NewModel()
	m.width = 100
	m.height = 30

	// Check status pane rendering includes Voice Chat card
	statusPane := m.renderStatusPane(45, 25)
	if !strings.Contains(statusPane, "VOICE CHAT:") {
		t.Errorf("expected status pane to include voice chat button, got:\n%s", statusPane)
	}
	if !strings.Contains(statusPane, "Whisper (ASR) ⇄ Pocket-TTS") {
		t.Errorf("expected status pane to include voice model note, got:\n%s", statusPane)
	}

	// Check footer includes Voice Chat keybinding
	footer := m.renderFooter()
	if !strings.Contains(footer, "Voice Chat") {
		t.Errorf("expected footer to include 'Voice Chat' keybinding, got:\n%s", footer)
	}

	// Check context binding when an agent task session is inspected
	m.currentInspection = &deck.SessionInspection{
		Instance: deck.InstanceRecord{
			ID:          "inst-voice-ctx",
			Title:       "Auth Worker",
			Tool:        "agy",
			Status:      "running",
			TmuxSession: "agentdeck_auth_ctx",
		},
	}
	ctxStatusPane := m.renderStatusPane(45, 25)
	if !strings.Contains(ctxStatusPane, "VOICE CHAT: Auth Worker") {
		t.Errorf("expected context card to show 'VOICE CHAT: Auth Worker', got:\n%s", ctxStatusPane)
	}
}

func TestUI_LeftRightArrowCollapsingAndExpanding(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 30
	m.focus = FocusSelection

	// Setup tree with 1 group and 2 sessions
	group := &deck.GroupNode{
		Path:     "backend",
		Name:     "Backend",
		Expanded: true,
		Depth:    0,
		Sessions: []deck.InstanceRecord{
			{ID: "sess-1", Title: "Auth Service", Tool: "agy"},
			{ID: "sess-2", Title: "DB Worker", Tool: "claude"},
		},
	}
	m.treeRoots = []*deck.GroupNode{group}
	m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
	m.selectedTreeIdx = 0 // On group "Backend"

	// 1. Press "left" (collapse group)
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model := mUpdated.(Model)

	if group.Expanded {
		t.Errorf("expected group 'Backend' to be collapsed after KeyLeft")
	}
	if len(model.treeItems) != 1 {
		t.Errorf("expected only 1 visible item when collapsed, got %d", len(model.treeItems))
	}

	// 2. Press "right" (expand group)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = mUpdated.(Model)

	if !group.Expanded {
		t.Errorf("expected group 'Backend' to be expanded after KeyRight")
	}
	if len(model.treeItems) != 3 {
		t.Errorf("expected 3 visible items when expanded, got %d", len(model.treeItems))
	}

	// 3. Press "right" again (step into first child)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRight})
	model = mUpdated.(Model)

	if model.selectedTreeIdx != 1 {
		t.Errorf("expected selectedTreeIdx 1 (Auth Service), got %d", model.selectedTreeIdx)
	}

	// 4. Press "left" on child session (jump up to parent group)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyLeft})
	model = mUpdated.(Model)

	if model.selectedTreeIdx != 0 {
		t.Errorf("expected selectedTreeIdx 0 (parent group), got %d", model.selectedTreeIdx)
	}
}

func TestUI_MouseClickSupport(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 30

	group := &deck.GroupNode{
		Path:     "frontend",
		Name:     "Frontend",
		Expanded: false,
		Depth:    0,
		Sessions: []deck.InstanceRecord{
			{ID: "sess-fe", Title: "Web UI", Tool: "claude"},
		},
	}
	m.treeRoots = []*deck.GroupNode{group}
	m.treeItems = deck.FlattenVisibleTree(m.treeRoots)
	m.selectedTreeIdx = 0

	// Click on group row (X: 5, Y: 4)
	clickMsg := tea.MouseMsg{
		X:      5,
		Y:      4,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	mUpdated, _ := m.Update(clickMsg)
	model := mUpdated.(Model)

	if !group.Expanded {
		t.Errorf("expected group to expand on mouse click")
	}
	if len(model.treeItems) != 2 {
		t.Errorf("expected 2 visible items after expanding on click, got %d", len(model.treeItems))
	}

	// Test mouse wheel down
	wheelDown := tea.MouseMsg{
		Button: tea.MouseButtonWheelDown,
	}
	mUpdated, _ = model.Update(wheelDown)
	model = mUpdated.(Model)

	if model.selectedTreeIdx != 1 {
		t.Errorf("expected cursor moved down to item 1 on wheel down, got %d", model.selectedTreeIdx)
	}

	// Test mouse wheel up
	wheelUp := tea.MouseMsg{
		Button: tea.MouseButtonWheelUp,
	}
	mUpdated, _ = model.Update(wheelUp)
	model = mUpdated.(Model)

	if model.selectedTreeIdx != 0 {
		t.Errorf("expected cursor moved up to item 0 on wheel up, got %d", model.selectedTreeIdx)
	}

	// 1. Single click on child session (Y: 5) selects it without attaching (cmd == nil)
	clickSess := tea.MouseMsg{
		X:      10,
		Y:      5,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	mUpdated, cmd := model.Update(clickSess)
	model = mUpdated.(Model)

	if model.selectedTreeIdx != 1 {
		t.Errorf("expected selectedTreeIdx 1 on click, got %d", model.selectedTreeIdx)
	}
	if cmd != nil {
		t.Errorf("expected cmd == nil on single click (select only), got non-nil cmd")
	}

	// 2. Click again on already selected session attaches to tmux (cmd != nil)
	mUpdated, cmd = model.Update(clickSess)
	model = mUpdated.(Model)
	if cmd == nil {
		t.Errorf("expected attach cmd != nil when clicking already selected session")
	}

	// 3. Click inside Pane 2 (The Task) at X: 50, Y: 10 switches focus to FocusTask
	clickPane2 := tea.MouseMsg{
		X:      50,
		Y:      10,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	mUpdated, _ = model.Update(clickPane2)
	model = mUpdated.(Model)
	if model.focus != FocusTask {
		t.Errorf("expected focus == FocusTask, got %v", model.focus)
	}

	// 4. Mouse wheel over Pane 2 scrolls taskScrollOffset
	wheelTask := tea.MouseMsg{
		X:      50,
		Y:      10,
		Button: tea.MouseButtonWheelUp,
	}
	mUpdated, _ = model.Update(wheelTask)
	model = mUpdated.(Model)
	if model.taskScrollOffset != 1 {
		t.Errorf("expected taskScrollOffset == 1 after wheel up in Pane 2, got %d", model.taskScrollOffset)
	}

	// 5. Click inside Pane 3 (Session Status) at X: 100, Y: 10 switches focus to FocusStatus
	clickPane3 := tea.MouseMsg{
		X:      100,
		Y:      10,
		Button: tea.MouseButtonLeft,
		Action: tea.MouseActionPress,
	}
	mUpdated, _ = model.Update(clickPane3)
	model = mUpdated.(Model)
	if model.focus != FocusStatus {
		t.Errorf("expected focus == FocusStatus, got %v", model.focus)
	}
}

func TestUI_CommandPalette_Routines_Voice_Truncation(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 35

	// 1. Test Command Palette [?]
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	m = mUpdated.(Model)
	if !m.showCommandPalette {
		t.Errorf("expected showCommandPalette to be true after '?'")
	}
	palView := m.renderCommandPalette(100, 25)
	if !strings.Contains(palView, "Global Command Palette") {
		t.Errorf("expected command palette title in view: %s", palView)
	}
	if !strings.Contains(palView, "Attach to Tmux Session") {
		t.Errorf("expected attach command in view: %s", palView)
	}

	// Esc closes palette
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(Model)
	if m.showCommandPalette {
		t.Errorf("expected showCommandPalette to be false after Esc")
	}

	// 2. Test Routines Dialog [r]
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = mUpdated.(Model)
	if !m.showRoutinesDialog {
		t.Errorf("expected showRoutinesDialog to be true after 'r'")
	}
	rtView := m.renderRoutinesDialog(100, 25)
	if !strings.Contains(rtView, "Scheduled Routines") {
		t.Errorf("expected routines title in view: %s", rtView)
	}
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(Model)
	if m.showRoutinesDialog {
		t.Errorf("expected showRoutinesDialog to be false after Esc")
	}

	// 3. Test Voice Dialog [v]
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'v'}})
	m = mUpdated.(Model)
	if !m.showVoiceDialog {
		t.Errorf("expected showVoiceDialog to be true after 'v'")
	}
	vcView := m.renderVoiceDialog(100, 25)
	if !strings.Contains(vcView, "Resident Voice") {
		t.Errorf("expected voice title in view: %s", vcView)
	}
	mUpdated, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = mUpdated.(Model)
	if m.showVoiceDialog {
		t.Errorf("expected showVoiceDialog to be false after Esc")
	}

	// 4. Test UTF-8 and ANSI safe truncation
	utf8Str := "🔥🚀 HyperFrames Video Production Engine"
	truncated := truncateString(utf8Str, 15)
	if !strings.HasSuffix(truncated, "...") {
		t.Errorf("expected ellipsis on truncated rune string, got %s", truncated)
	}

	ansiLine := "\x1b[32m[SUCCESS]\x1b[0m Task completed successfully with output verification"
	visLine := truncateVisibleLine(ansiLine, 20)
	if !strings.HasSuffix(visLine, "\x1b[0m") {
		t.Errorf("expected ANSI reset at end of line: %q", visLine)
	}
}

func TestUI_AccountDisplayAcrossPanes(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 30
	m.accounts = []rotation.AccountStatus{
		{ID: "danielmcf15@gmail.com", Email: "danielmcf15@gmail.com", RemainingQuotaPct: 55.3},
		{ID: "clownfishexperience@gmail.com", Email: "clownfishexperience@gmail.com", RemainingQuotaPct: 99.3},
	}

	inst := &deck.InstanceRecord{
		ID:        "inst-moraaky-test",
		Title:     "MoraakyAI",
		Tool:      "agy",
		Status:    "running",
		Account:   "danielmcf15@gmail.com",
		GroupPath: "Production/Core",
	}

	m.treeItems = []deck.TreeItem{
		{
			IsGroup: false,
			Session: inst,
			Depth:   0,
		},
	}
	m.selectedTreeIdx = 0
	m.currentInspection = deck.FastInspectionFromRecord(inst)

	// 1. Test Pane 1 (Selection Tree) contains account badge
	pane1 := m.renderSelectionPane(45, 15)
	if !strings.Contains(pane1, "@danielmcf") {
		t.Errorf("expected Pane 1 to display '@danielmcf', got:\n%s", pane1)
	}

	// 2. Test Pane 2 (The Task Header) contains full account badge
	pane2 := m.renderTaskPane(45, 15)
	if !strings.Contains(pane2, "@danielmcf15@gmail.com") {
		t.Errorf("expected Pane 2 to display '@danielmcf15@gmail.com', got:\n%s", pane2)
	}

	// 3. Test Pane 3 (Status Pane) matches account in Identity and Quota
	lines, _ := m.buildStatusPaneLines(50)
	statusText := strings.Join(lines, "\n")
	if !strings.Contains(statusText, "danielmcf15@gmail.com") {
		t.Errorf("expected Pane 3 to display danielmcf15@gmail.com, got:\n%s", statusText)
	}
	if !strings.Contains(statusText, "ACTIVE QUOTA (danielmcf15@gmail.com)") {
		t.Errorf("expected Pane 3 Active Quota for danielmcf15@gmail.com, got:\n%s", statusText)
	}

	// 4. Test Global Header shows target task account
	header := m.renderHeader()
	if !strings.Contains(header, "danielmcf15@gmail.com") {
		t.Errorf("expected Global Header to display danielmcf15@gmail.com, got:\n%s", header)
	}

	// 5. Test pressing [a] pre-selects correct account modal index
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = mUpdated.(Model)
	if !m.showAccountDialog {
		t.Errorf("expected showAccountDialog to be true")
	}
	if m.accountModalIdx != 0 {
		t.Errorf("expected accountModalIdx 0 (danielmcf15@gmail.com), got %d", m.accountModalIdx)
	}
}

func TestUI_NewTaskModal_HotkeysAndAutocomplete(t *testing.T) {
	tmpDir := t.TempDir()
	sub1 := filepath.Join(tmpDir, "backend-service")
	sub2 := filepath.Join(tmpDir, "backend-worker")
	_ = os.MkdirAll(sub1, 0755)
	_ = os.MkdirAll(sub2, 0755)

	m := NewModel()
	m.width = 120
	m.height = 35

	// Open dialog
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}})
	model := mUpdated.(Model)

	// 1. Move to Path field (field 1)
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.newTaskFieldFocus != 1 {
		t.Fatalf("expected focus on field 1, got %d", model.newTaskFieldFocus)
	}

	// 2. Set partial path to tmpDir/back and Tab to autocomplete
	model.newTaskPathInput.Set(filepath.Join(tmpDir, "back"))
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)

	if !model.newTaskPathCycler.IsActive() {
		t.Errorf("expected pathCycler to be active after Tab on partial match")
	}
	if model.newTaskPathInput.Value != sub1 && model.newTaskPathInput.Value != sub2 {
		t.Errorf("expected autocompleted path to be sub1 or sub2, got %q", model.newTaskPathInput.Value)
	}

	// Tab again cycles to second match
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.newTaskPathInput.Value != sub1 && model.newTaskPathInput.Value != sub2 {
		t.Errorf("expected autocompleted second match, got %q", model.newTaskPathInput.Value)
	}

	// 3. Test Ctrl+W path-aware rubout
	model.newTaskPathInput.Set("/home/falluba/projects/service")
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlW})
	model = mUpdated.(Model)
	if model.newTaskPathInput.Value != "/home/falluba/projects/" {
		t.Errorf("expected '/home/falluba/projects/', got %q", model.newTaskPathInput.Value)
	}

	// 4. Test Ctrl+R cycles recent paths
	model.newTaskRecentPaths = []string{"/workspace/alpha", "/workspace/beta"}
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlR})
	model = mUpdated.(Model)
	if model.newTaskPathInput.Value != "/workspace/beta" {
		t.Errorf("expected recent path '/workspace/beta', got %q", model.newTaskPathInput.Value)
	}

	// 5. Test jump to Harness field (field 3) and number shortcuts
	model.newTaskFieldFocus = 3
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	model = mUpdated.(Model)
	if model.newTaskToolIdx != 2 { // '3' -> index 2 (codex)
		t.Errorf("expected tool idx 2 (codex), got %d", model.newTaskToolIdx)
	}

	// 6. Test Ctrl+U clears line
	model.newTaskFieldFocus = 0
	model.newTaskTitleInput.Set("Temporary Name")
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyCtrlU})
	model = mUpdated.(Model)
	if model.newTaskTitleInput.Value != "" {
		t.Errorf("expected cleared title after Ctrl+U, got %q", model.newTaskTitleInput.Value)
	}

	// 7. Render dialog with suggestion chips
	dialog := model.renderNewTaskDialog(80, 25)
	if !strings.Contains(dialog, "New Task") {
		t.Errorf("expected dialog output, got:\n%s", dialog)
	}
	if !strings.Contains(dialog, "[Ctrl+S] Create Task") {
		t.Errorf("expected hotkey bar in dialog, got:\n%s", dialog)
	}
}

func TestSwitchHarnessDialogUI(t *testing.T) {
	m := NewModel()
	m.width = 120
	m.height = 35

	// Setup a session in the tree
	sess := &deck.InstanceRecord{
		ID:        "sess-switch-1",
		Title:     "Backend API",
		Tool:      "codex",
		GroupPath: "services",
		Account:   "danielmcf15@gmail.com",
	}
	m.treeItems = []deck.TreeItem{
		{IsGroup: false, Session: sess},
	}
	m.selectedTreeIdx = 0

	// 1. Press 'w' to open Switch Harness Dialog
	mUpdated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'w'}})
	model := mUpdated.(Model)

	if !model.showSwitchDialog {
		t.Fatalf("expected showSwitchDialog to be true after pressing 'w'")
	}
	if model.switchTargetInst == nil || model.switchTargetInst.ID != "sess-switch-1" {
		t.Fatalf("expected switchTargetInst to be sess-switch-1, got %v", model.switchTargetInst)
	}
	if model.switchCreateNewTask {
		t.Errorf("expected switchCreateNewTask to default to false (in-place recreation)")
	}

	// 2. Press 'm' to toggle Mode to Fork New Task
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	model = mUpdated.(Model)
	if !model.switchCreateNewTask {
		t.Errorf("expected switchCreateNewTask to be true after pressing 'm'")
	}

	// Press 'f' to toggle back
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	model = mUpdated.(Model)
	if model.switchCreateNewTask {
		t.Errorf("expected switchCreateNewTask to be false after pressing 'f'")
	}

	// 3. Test number keys 1-5 for harness selection
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'5'}})
	model = mUpdated.(Model)
	if model.switchTargetToolIdx != 4 {
		t.Errorf("expected targetToolIdx 4 (vox-studio) for key '5', got %d", model.switchTargetToolIdx)
	}

	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	model = mUpdated.(Model)
	if model.switchTargetToolIdx != 0 {
		t.Errorf("expected targetToolIdx 0 (agy) for key '1', got %d", model.switchTargetToolIdx)
	}

	// 4. Test toggle handoff with 'h'
	initHandoff := model.switchWithHandoff
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}})
	model = mUpdated.(Model)
	if model.switchWithHandoff == initHandoff {
		t.Errorf("expected switchWithHandoff to toggle with 'h'")
	}

	// 5. Test rendering dialog
	rendered := model.renderSwitchDialog(80, 25)
	if !strings.Contains(rendered, "Switch Agent for \"Backend API\"") {
		t.Errorf("expected dialog title in output, got:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Switch Mode:") || !strings.Contains(rendered, "Recreate Session") {
		t.Errorf("expected switch mode in rendered dialog, got:\n%s", rendered)
	}

	// 6. Test Esc to cancel
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	model = mUpdated.(Model)
	if model.showSwitchDialog {
		t.Errorf("expected showSwitchDialog to be false after Esc")
	}
}
