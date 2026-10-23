package main

import (
	"database/sql"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestSessionAttachResolvesSavedIdentifiers(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	_, err = db.Exec(`CREATE TABLE instances (
		id TEXT, title TEXT, project_path TEXT, group_path TEXT, sort_order INTEGER,
		command TEXT, wrapper TEXT, tool TEXT, status TEXT, tmux_session TEXT,
		created_at INTEGER, last_accessed INTEGER, account TEXT, tool_data TEXT);
		INSERT INTO instances VALUES ('saved-id', 'Saved task', '/tmp', '', 0,
		'/bin/sh', '', 'shell', 'idle', 'actual-tmux-name', 0, 0, '', '{}');`)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte("#!/bin/sh\ncase \"$*\" in *display-message*) echo 0;; esac\n"), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("AGENTDECK_STATE_DB", dbPath)
	t.Setenv("TMUX", "")
	for _, identifier := range []string{"saved-id", "Saved task", "actual-tmux-name"} {
		cmd, err := sessionAttachCommand(identifier)
		if err != nil {
			t.Fatal(err)
		}
		want := []string{"tmux", "-L", "default", "attach-session", "-t", "=actual-tmux-name"}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Errorf("%q attached to %v; want %v", identifier, cmd.Args, want)
		}
	}
	cmd, err := sessionAttachCommand("unmanaged-session")
	if err != nil || !reflect.DeepEqual(cmd.Args, []string{"tmux", "attach-session", "-t", "=unmanaged-session"}) {
		t.Fatalf("raw tmux session fallback failed: %v, %v", cmd, err)
	}
}

func TestSessionAttachWithoutDatabase(t *testing.T) {
	t.Setenv("AGENTDECK_STATE_DB", filepath.Join(t.TempDir(), "absent.db"))
	t.Setenv("TMUX", "")
	cmd, err := sessionAttachCommand("raw-session")
	if err != nil || cmd.Args[len(cmd.Args)-1] != "=raw-session" {
		t.Fatalf("cannot attach to unmanaged session without database: %v, %v", cmd, err)
	}
}
