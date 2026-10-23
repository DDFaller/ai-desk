package ui

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "ai-desk-ui-tests-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", root)
	_ = os.Setenv("AGENTDECK_STATE_DB", filepath.Join(root, "absent.db"))
	_ = os.Setenv("AI_DESK_AGENTS_DIR", filepath.Join(root, "agents"))
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0700)
	_ = os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 1\n"), 0700)
	_ = os.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
