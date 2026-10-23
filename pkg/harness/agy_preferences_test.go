package harness

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestAgyPreferencesSurviveReplacement(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("AI_DESK_USER_HOME", home)
	write := func(root, relative, data string) {
		t.Helper()
		path := filepath.Join(root, relative)
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	const settings = ".gemini/antigravity-cli/settings.json"
	const onboarding = ".gemini/antigravity-cli/cache/nux_state.json"
	write(home, settings, `{"agentMode":"plan","enableTerminalSandbox":true,"toolPermission":"ask"}`)
	write(home, onboarding, `{"dismissed":["welcome"]}`)
	v := NewVirtualizer(t.TempDir(), t.TempDir()).ForSession("original")
	if _, err := v.PrepareSandboxWithSkills("agy", "first", nil); err != nil {
		t.Fatal(err)
	}
	first := v.SandboxPath("agy", "first")
	// User changes preferences inside the first session.
	write(first, settings, `{"agentMode":"custom","enableTerminalSandbox":false,"trustedWorkspaces":["/my-project"]}`)
	if _, err := v.PrepareSandboxWithSkills("agy", "first", nil); err != nil {
		t.Fatal(err)
	}
	next := NewVirtualizer(t.TempDir(), t.TempDir()).ForSession("replacement")
	second := next.SandboxPath("agy", "second")
	if err := InheritAgyPreferences(first, second); err != nil {
		t.Fatal(err)
	}
	if _, err := next.PrepareSandboxWithSkills("agy", "second", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(second, settings))
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["agentMode"] != "custom" || got["enableTerminalSandbox"] != false || got["toolPermission"] != "ask" {
		t.Fatalf("preferences lost: %s", data)
	}
	if _, err := os.Stat(filepath.Join(second, onboarding)); err != nil {
		t.Fatal(err)
	}
	write(second, settings, `{}`)
	data, err = os.ReadFile(filepath.Join(first, settings))
	if err != nil || string(data) == `{}` {
		t.Fatal("replacement modified original preferences")
	}
}

func TestAgyPreferencesRejectMalformedJSON(t *testing.T) {
	source, target := t.TempDir(), t.TempDir()
	dir := filepath.Join(source, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "settings.json"), []byte(`{broken`), 0600); err != nil {
		t.Fatal(err)
	}
	if err := InheritAgyPreferences(source, target); err == nil {
		t.Fatal("expected invalid settings error")
	}
}
