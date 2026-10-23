package ui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

func TestSessionEntryPointsPrepareRecovery(t *testing.T) {
	inst := &deck.InstanceRecord{ID: "saved-session", Title: "Saved task"}
	for _, entry := range []string{"keyboard", "mouse", "inspector"} {
		t.Run(entry, func(t *testing.T) {
			m := Model{
				width: 140, height: 35, focus: FocusSelection,
				treeItems:    []deck.TreeItem{{Session: inst}},
				lastClickIdx: -1,
			}
			var msg tea.Msg = tea.KeyMsg{Type: tea.KeyEnter}
			switch entry {
			case "inspector":
				m.showInspector = true
				m.currentInspection = &deck.SessionInspection{Instance: *inst}
			case "mouse":
				msg = tea.MouseMsg{X: 10, Y: 4, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress}
			}
			updated, cmd := m.Update(msg)
			if cmd == nil {
				t.Fatal("opening a saved session did not prepare recovery")
			}
			// Preparation must precede interactive attachment, even with no
			// saved tmux name. A missing DB must surface as a recovery error.
			ready, ok := cmd().(sessionAttachReadyMsg)
			if !ok || ready.err == nil {
				t.Fatalf("expected recovery result, got %#v", ready)
			}
			result, next := updated.(Model).Update(ready)
			if next != nil || !strings.Contains(result.(Model).toast, "database unavailable") {
				t.Fatal("recovery failure did not reach the UI")
			}
		})
	}
}

func TestAttachFinishedReportsFailure(t *testing.T) {
	// Keep reload independent of the user's sessions and account vault.
	t.Setenv("AGENTDECK_STATE_DB", filepath.Join(t.TempDir(), "absent.db"))
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	for _, failure := range []error{nil, errors.New("terminal unavailable")} {
		m := Model{sb: swapbridge.NewSwapBridge(t.TempDir())}
		updated, _ := m.Update(attachFinishedMsg{err: failure})
		toast := updated.(Model).toast
		if failure == nil {
			if toast != "Detached from session." {
				t.Fatalf("unexpected detach message: %q", toast)
			}
		} else if !strings.Contains(toast, failure.Error()) || strings.Contains(toast, "Detached") {
			t.Fatalf("attach failure was hidden: %q", toast)
		}
	}
}
