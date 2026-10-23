package configpaths

import "testing"

func TestHomeOutsideManagedSession(t *testing.T) {
	t.Setenv("AI_DESK_USER_HOME", "")
	t.Setenv("HOME", "/home/operator/.ai-desk/harnesses/sessions/abc/codex/default")
	home, err := HomeDir()
	if err != nil || home != "/home/operator" {
		t.Fatalf("%q %v", home, err)
	}
	t.Setenv("AI_DESK_USER_HOME", "/custom/operator")
	home, _ = HomeDir()
	if home != "/custom/operator" {
		t.Fatal(home)
	}
}
