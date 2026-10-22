package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestUI_ModelLifecycle(t *testing.T) {
	m := NewModel()

	// 1. Send WindowSizeMsg
	mUpdated, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model := mUpdated.(Model)

	// 2. Render View
	view := model.View()
	if !strings.Contains(view, "AI DESK") {
		t.Errorf("expected view to contain 'AI DESK', got:\n%s", view)
	}
	if !strings.Contains(view, "Active Tmux Sessions") {
		t.Errorf("expected view to contain 'Active Tmux Sessions', got:\n%s", view)
	}

	// 3. Toggle pane focus with Tab
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.focus != FocusAccounts {
		t.Errorf("expected focus on Accounts pane after Tab, got %v", model.focus)
	}

	// 4. Test Quit key
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected quit command on 'q'")
	}
}
