package ui

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/falluba/ai-desk/pkg/deck"
)

func TestTaskAutocomplete_DirectoryCompletions(t *testing.T) {
	tmpDir := t.TempDir()
	subA := filepath.Join(tmpDir, "alpha-service")
	subB := filepath.Join(tmpDir, "beta-api")
	subC := filepath.Join(tmpDir, "alpha-client")
	_ = os.MkdirAll(subA, 0755)
	_ = os.MkdirAll(subB, 0755)
	_ = os.MkdirAll(subC, 0755)

	// 1. Partial prefix match
	matches, err := GetDirectoryCompletions(filepath.Join(tmpDir, "alpha"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(matches) != 2 {
		t.Errorf("expected 2 matches for 'alpha', got %d: %v", len(matches), matches)
	}

	// 2. Trailing slash list directory contents
	allMatches, err := GetDirectoryCompletions(tmpDir + string(os.PathSeparator))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(allMatches) != 3 {
		t.Errorf("expected 3 directories in tmpDir, got %d: %v", len(allMatches), allMatches)
	}
}

func TestTaskAutocomplete_CompletionCycler(t *testing.T) {
	c := &CompletionCycler{}
	if c.IsActive() {
		t.Errorf("expected inactive cycler initially")
	}

	c.SetMatches([]string{"/dir/one", "/dir/two", "/dir/three"})
	if !c.IsActive() {
		t.Errorf("expected active cycler after SetMatches")
	}

	first := c.Next()
	if first != "/dir/one" {
		t.Errorf("expected '/dir/one', got %s", first)
	}
	second := c.Next()
	if second != "/dir/two" {
		t.Errorf("expected '/dir/two', got %s", second)
	}
	third := c.Next()
	if third != "/dir/three" {
		t.Errorf("expected '/dir/three', got %s", third)
	}
	wrap := c.Next()
	if wrap != "/dir/one" {
		t.Errorf("expected wrap around to '/dir/one', got %s", wrap)
	}

	prev := c.Prev()
	if prev != "/dir/three" {
		t.Errorf("expected prev to wrap to '/dir/three', got %s", prev)
	}
}

func TestTaskAutocomplete_GroupCompletions(t *testing.T) {
	groups := []string{"Production/Core", "Production/Worker", "Creative/VideoStudio", "my-sessions"}

	matches := GetGroupCompletions("Prod", groups)
	if len(matches) != 2 {
		t.Errorf("expected 2 Production groups, got %d: %v", len(matches), matches)
	}

	all := GetGroupCompletions("", groups)
	if len(all) != 4 {
		t.Errorf("expected 4 groups, got %d: %v", len(all), all)
	}
}

func TestTaskAutocomplete_RecentProjectPaths(t *testing.T) {
	insts := []deck.InstanceRecord{
		{ID: "1", ProjectPath: "/path/one", LastAccessed: 100},
		{ID: "2", ProjectPath: "/path/two", LastAccessed: 300},
		{ID: "3", ProjectPath: "/path/one", LastAccessed: 200}, // duplicate
	}
	recent := GetRecentProjectPaths(insts)
	if len(recent) < 2 {
		t.Fatalf("expected at least 2 distinct paths, got %d", len(recent))
	}
}

func TestTextInputState_Operations(t *testing.T) {
	ti := &TextInputState{}
	ti.Set("/home/falluba/projects/auth-api")

	if ti.Cursor != 31 {
		t.Errorf("expected cursor at 31, got %d", ti.Cursor)
	}

	// Test DeleteWordBackwardPath (like agent-deck #896)
	ti.DeleteWordBackwardPath()
	if ti.Value != "/home/falluba/projects/" {
		t.Errorf("expected '/home/falluba/projects/', got %q", ti.Value)
	}

	ti.DeleteWordBackwardPath()
	if ti.Value != "/home/falluba/" {
		t.Errorf("expected '/home/falluba/', got %q", ti.Value)
	}

	// Test cursor movement & editing
	ti.Home()
	if ti.Cursor != 0 {
		t.Errorf("expected Home to set cursor to 0")
	}

	ti.Insert('~')
	if ti.Value != "~/home/falluba/" {
		t.Errorf("expected '~/home/falluba/', got %q", ti.Value)
	}

	ti.Backspace()
	if ti.Value != "/home/falluba/" {
		t.Errorf("expected '/home/falluba/', got %q", ti.Value)
	}
}
