package deck

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/falluba/ai-desk/pkg/harness"
)

func fakeAttachTmux(t *testing.T, state string) string {
	t.Helper()
	dir := t.TempDir()
	statePath, logPath := filepath.Join(dir, "state"), filepath.Join(dir, "calls")
	if err := os.WriteFile(statePath, []byte(state), 0600); err != nil {
		t.Fatal(err)
	}
	script := `#!/bin/sh
printf '%s\n' "$@" >> "$TEST_TMUX_LOG"
shift 2
state=$(cat "$TEST_TMUX_STATE")
case "$1" in
has-session)
  case "$state" in
    missing) echo "can't find session: test-session" >&2; exit 1 ;;
    stopped) echo 'no server running on /tmp/test/default' >&2; exit 1 ;;
    denied) echo 'error connecting to /tmp/test/default (Permission denied)' >&2; exit 1 ;;
    missing-binary) exit 127 ;;
  esac ;;
display-message)
  if [ "$state" = dead ]; then echo 1; else echo 0; fi ;;
new-session|respawn-pane)
  if [ "$TEST_TMUX_FAIL_SPAWN" = 1 ]; then echo 'bad working directory' >&2; exit 1; fi
  printf live > "$TEST_TMUX_STATE" ;;
bind-key|set-option) ;;
*) echo "unexpected tmux command: $1" >&2; exit 2 ;;
esac
`
	if err := os.WriteFile(filepath.Join(dir, "tmux"), []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("TEST_TMUX_STATE", statePath)
	t.Setenv("TEST_TMUX_LOG", logPath)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func insertRecoverySession(t *testing.T, sdb *StateDB, command, metadata string) {
	t.Helper()
	_, err := sdb.db.Exec(`INSERT INTO instances (id, title, project_path, tool, command, tmux_session, tool_data, account)
		VALUES ('task-id', 'Test task', ?, 'agy', ?, 'test-session', ?, 'test-account')`, t.TempDir(), command, metadata)
	if err != nil {
		t.Fatal(err)
	}
}

func TestPrepareSessionAttach(t *testing.T) {
	for _, state := range []string{"live", "missing", "stopped", "dead", "denied", "missing-binary"} {
		t.Run(state, func(t *testing.T) {
			logPath := fakeAttachTmux(t, state)
			sdb, _ := setupTestDB(t)
			defer sdb.Close()
			insertRecoverySession(t, sdb, "agy", `{}`)
			cmd, err := sdb.PrepareSessionAttach(nil, "task-id")
			calls, _ := os.ReadFile(logPath)
			log := string(calls)
			if state == "denied" || state == "missing-binary" {
				if err == nil || cmd != nil {
					t.Fatalf("expected probe failure, got cmd=%v err=%v", cmd, err)
				}
				if strings.Contains(log, "new-session") || strings.Contains(log, "respawn-pane") {
					t.Fatal("probe error must not restart a session")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			want := []string{"tmux", "-L", "default", "attach-session", "-t", "=test-session"}
			if !reflect.DeepEqual(cmd.Args, want) {
				t.Fatalf("attach args = %v, want %v", cmd.Args, want)
			}
			if state == "live" {
				if strings.Contains(log, "new-session") || strings.Contains(log, "respawn-pane") {
					t.Fatal("live detached session was restarted")
				}
			} else {
				operation := "new-session"
				if state == "dead" {
					operation = "respawn-pane"
				}
				if !strings.Contains(log, operation+"\n") || !strings.Contains(log, "agy --continue\n") {
					t.Fatalf("missing recovery command in %s", log)
				}
				if strings.Contains(log, "\n-k\n") {
					t.Fatal("recovery must not forcibly kill a pane")
				}
			}
			inst, _ := sdb.FindInstance("task-id")
			if inst.LastAccessed == 0 {
				t.Fatal("successful attachment did not update access time")
			}
		})
	}
}

func TestPrepareSessionAttachConfiguresSafeHotkeys(t *testing.T) {
	logPath := fakeAttachTmux(t, "live")
	sdb, _ := setupTestDB(t)
	defer sdb.Close()
	insertRecoverySession(t, sdb, "agy", `{}`)
	_, err := sdb.PrepareSessionAttach(nil, "task-id")
	if err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(logPath)
	log := string(calls)
	if !strings.Contains(log, "bind-key\n-n\nC-q\ndetach-client\n") {
		t.Fatalf("missing C-q detach-client binding in calls: %s", log)
	}
	if !strings.Contains(log, "set-option\n-t\ntest-session\nprefix2\nC-a\n") {
		t.Fatalf("missing prefix2 C-a configuration in calls: %s", log)
	}
}

func TestPrepareSessionAttachPreservesAccountAndSkills(t *testing.T) {
	logPath := fakeAttachTmux(t, "missing")
	sdb, _ := setupTestDB(t)
	defer sdb.Close()
	insertRecoverySession(t, sdb, "agy", `{"created_by":"ai-desk","initial_prompt":"do not replay"}`)
	virt := harness.NewVirtualizer(t.TempDir(), t.TempDir())
	if err := os.MkdirAll(filepath.Join(virt.SkillsPath(), "extra-skill"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := virt.PrepareSandboxWithSkills("agy", "test-account", []string{"extra-skill"}); err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.PrepareSessionAttach(virt, "Test task"); err != nil {
		t.Fatal(err)
	}
	calls, _ := os.ReadFile(logPath)
	for _, want := range []string{"HOME=" + virt.SandboxPath("agy", "test-account"), "GEMINI_CLI_HOME=" + virt.SandboxPath("agy", "test-account"), "AI_DESK_ACCOUNT=test-account"} {
		if !strings.Contains(string(calls), "\n"+want+"\n") {
			t.Errorf("missing restored environment %q", want)
		}
	}
	if strings.Contains(string(calls), "do not replay") {
		t.Fatal("recovery replayed the initial prompt")
	}
	skills, err := virt.ListAttachedSkills("agy", "test-account")
	if err != nil || !reflect.DeepEqual(skills, []string{"extra-skill"}) {
		t.Fatalf("recovery changed attached skills: %v (%v)", skills, err)
	}
}

func TestPrepareSessionAttachSpawnFailure(t *testing.T) {
	fakeAttachTmux(t, "missing")
	t.Setenv("TEST_TMUX_FAIL_SPAWN", "1")
	sdb, _ := setupTestDB(t)
	defer sdb.Close()
	insertRecoverySession(t, sdb, "agy", `{}`)
	cmd, err := sdb.PrepareSessionAttach(nil, "task-id")
	if cmd != nil || err == nil || !strings.Contains(err.Error(), "bad working directory") {
		t.Fatalf("lost recovery error: cmd=%v err=%v", cmd, err)
	}
	inst, _ := sdb.FindInstance("task-id")
	if inst.LastAccessed != 0 {
		t.Fatal("failed recovery was recorded as successful")
	}
}

func TestRecoveryCommandPreservesConversationAndWrapper(t *testing.T) {
	for _, tc := range []struct{ tool, metadata, want string }{
		{"codex", `{"codex_session_id":"saved-id"}`, "codex resume 'saved-id'"},
		{"claude", `{"claude_session_id":"saved-id"}`, "claude --resume 'saved-id'"},
		{"opencode", `{"opencode_session_id":"saved-id"}`, "opencode --session 'saved-id'"},
		{"gemini", `{"gemini_session_id":"saved-id"}`, "gemini --resume 'saved-id'"},
		{"agy", `{}`, "agy --continue"},
		{"agy", `{"is_fresh_harness":true}`, "agy"},
		{"codex", `{"codex_session_id":"saved-id","is_fresh_harness":true}`, "codex"},
		{"codex", `{}`, "codex"},
	} {
		t.Run(tc.tool+tc.metadata, func(t *testing.T) {
			inst := &InstanceRecord{Tool: tc.tool, Command: tc.tool, ToolData: tc.metadata, Wrapper: "wrapper {command} --sandbox workspace-write"}
			got, env, err := recoveryCommand(nil, inst)
			want := "wrapper " + tc.want + " --sandbox workspace-write"
			if err != nil || got != want || len(env) != 0 {
				t.Fatalf("got %q, %v, %v; want %q with host environment", got, env, err, want)
			}
		})
	}
	inst := &InstanceRecord{Tool: "agy", Command: "agy --conversation already-selected", ToolData: `{}`}
	got, _, err := recoveryCommand(nil, inst)
	if err != nil || got != inst.Command {
		t.Fatalf("custom recovery command changed: %q, %v", got, err)
	}
}

func TestTmuxAttachCommandInsideTmux(t *testing.T) {
	t.Setenv("TMUX", "/tmp/outer-socket,123,0")
	t.Setenv("TMUX_PANE", "%1")
	for _, socket := range []string{"", "custom-server"} {
		cmd := TmuxAttachCommand("target", socket)
		for _, env := range cmd.Env {
			if strings.HasPrefix(env, "TMUX=") || strings.HasPrefix(env, "TMUX_PANE=") {
				t.Fatalf("nested tmux attach inherited %s", env)
			}
		}
		want := []string{"tmux", "-S", "/tmp/outer-socket", "attach-session", "-t", "=target"}
		if socket != "" {
			want[1], want[2] = "-L", socket
		}
		if !reflect.DeepEqual(cmd.Args, want) {
			t.Fatalf("attach changed server: %v, want %v", cmd.Args, want)
		}
	}
}

// Run explicitly with AI_DESK_TMUX_INTEGRATION=1. This uses its own tmux
// server and database and never attaches to or alters the user's sessions.
func TestSessionAttachTmuxIntegration(t *testing.T) {
	if os.Getenv("AI_DESK_TMUX_INTEGRATION") != "1" {
		t.Skip("set AI_DESK_TMUX_INTEGRATION=1 to exercise a real isolated tmux server")
	}
	socket := fmt.Sprintf("ai-desk-recovery-test-%d", os.Getpid())
	tmux := func(args ...string) string {
		t.Helper()
		out, err := exec.Command("tmux", append([]string{"-L", socket}, args...)...).CombinedOutput()
		if err != nil {
			t.Fatalf("tmux %v: %v: %s", args, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	tmux("-f", "/dev/null", "new-session", "-d", "-s", "keeper", "/bin/sh")
	t.Cleanup(func() { _ = exec.Command("tmux", "-L", socket, "kill-server").Run() })
	sdb, _ := setupTestDB(t)
	defer sdb.Close()
	insertRecoverySession(t, sdb, "/bin/sh", `{}`)
	if _, err := sdb.db.Exec(`ALTER TABLE instances ADD COLUMN tmux_socket_name TEXT NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	if _, err := sdb.db.Exec(`UPDATE instances SET tmux_socket_name = ?`, socket); err != nil {
		t.Fatal(err)
	}
	prepare := func() {
		t.Helper()
		cmd, err := sdb.PrepareSessionAttach(nil, "task-id")
		if err != nil {
			t.Fatal(err)
		}
		if cmd.Args[2] != socket {
			t.Fatal("attach lost the saved tmux socket")
		}
	}
	// An overlapping name must not be mistaken for the saved session.
	tmux("new-session", "-d", "-s", "test-session-other", "/bin/sh")
	prepare()
	pid := tmux("display-message", "-p", "-t", "=test-session:", "#{pane_pid}")
	if attached := tmux("display-message", "-p", "-t", "=test-session:", "#{session_attached}"); attached != "0" {
		t.Fatal("expected a detached session")
	}
	prepare()
	if got := tmux("display-message", "-p", "-t", "=test-session:", "#{pane_pid}"); got != pid {
		t.Fatal("reopening a detached session replaced its process")
	}
	// Recreate a dead pane retained by remain-on-exit.
	tmux("set-option", "-w", "-t", "=test-session:", "remain-on-exit", "on")
	tmux("respawn-pane", "-k", "-t", "=test-session:", "/bin/true")
	// Synchronize with process exit using tmux wait-for, without polling.
	tmux("set-hook", "-t", "=test-session:", "pane-died", "wait-for -S recovery-dead")
	if tmux("display-message", "-p", "-t", "=test-session:", "#{pane_dead}") != "1" {
		tmux("wait-for", "recovery-dead")
	}
	prepare()
	if tmux("display-message", "-p", "-t", "=test-session:", "#{pane_dead}") != "0" {
		t.Fatal("dead pane was not recovered")
	}
}
