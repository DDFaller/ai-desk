package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
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
	// Check all 3 panes are present in the view
	if !strings.Contains(view, "Selection") {
		t.Errorf("expected view to contain Pane 1 'Selection', got:\n%s", view)
	}
	if !strings.Contains(view, "Task") {
		t.Errorf("expected view to contain Pane 2 'Task', got:\n%s", view)
	}
	if !strings.Contains(view, "Session Status") {
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

	// 5. Test Quit key
	_, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	if cmd == nil {
		t.Errorf("expected quit command on 'q'")
	}
}
