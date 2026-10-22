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
	if !strings.Contains(view, "Sessions") {
		t.Errorf("expected view to contain 'Sessions', got:\n%s", view)
	}

	// 3. Toggle pane focus with Tab
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)
	if model.focus != FocusAccounts {
		t.Errorf("expected focus on Accounts pane after Tab, got %v", model.focus)
	}

	// Switch back to Sessions
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyTab})
	model = mUpdated.(Model)

	// Test Inspector toggle on session
	mUpdated, _ = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	model = mUpdated.(Model)

	// Test Delete dialog open and cancel
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

	// 4. Test Quit key
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected quit command on 'q'")
	}
}
