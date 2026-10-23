package deck

import (
	"os"
	"path/filepath"
	"testing"
)

// Unit tests must never launch or alter a user's real sessions and credentials.
func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "ai-desk-tests-")
	if err != nil {
		panic(err)
	}
	_ = os.Setenv("HOME", root)
	_ = os.Setenv("AI_DESK_ACCOUNTS_DIR", filepath.Join(root, "accounts"))
	_ = os.Setenv("AI_DESK_HARNESSES_DIR", filepath.Join(root, "harnesses"))
	bin := filepath.Join(root, "bin")
	_ = os.MkdirAll(bin, 0700)
	_ = os.WriteFile(filepath.Join(bin, "tmux"), []byte("#!/bin/sh\nexit 0\n"), 0700)
	_ = os.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	agy := filepath.Join(root, ".gemini", "agy-swap")
	_ = os.MkdirAll(agy, 0700)
	_ = os.WriteFile(filepath.Join(agy, "accounts.json"), []byte(`{"tester@example.com":{"email":"tester@example.com"},"danielmcf15@gmail.com":{"email":"danielmcf15@gmail.com","quota_snapshot":{"groups":[{"buckets":[{"remaining_fraction":0.5}]}]}}}`), 0600)
	_ = os.WriteFile(filepath.Join(agy, "vault.json"), []byte(`{"test-account":{"access_token":"fixture"},"tester@example.com":{"access_token":"fixture"},"danielmcf15@gmail.com":{"access_token":"fixture"}}`), 0600)
	_ = os.WriteFile(filepath.Join(root, ".gemini", "google_accounts.json"), []byte(`{"active":"danielmcf15@gmail.com"}`), 0600)
	for _, id := range []string{"clownfishexperience@gmail.com", "new@example.com"} {
		dir := filepath.Join(root, "accounts", "codex", id)
		_ = os.MkdirAll(dir, 0700)
		_ = os.WriteFile(filepath.Join(dir, "auth.json"), []byte(`{"OPENAI_API_KEY":"fixture"}`), 0600)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
