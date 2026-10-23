package deck

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/harness"
)

// PrepareSessionAttach reuses a live session, or restores its saved command and
// account environment when tmux has exited. It never respawns a live pane.
func (s *StateDB) PrepareSessionAttach(virt *harness.Virtualizer, identifier string) (*exec.Cmd, error) {
	inst, err := s.FindInstance(identifier)
	if err != nil {
		return nil, err
	}
	socket, err := s.tmuxSocket(inst)
	if err != nil {
		return nil, err
	}

	name := inst.TmuxSession
	if name == "" {
		name = inst.ID
	}
	target := "=" + name       // tmux otherwise accepts prefixes and patterns.
	paneTarget := target + ":" // Qualify the session when a command expects a pane.
	run := func(args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		out, err := exec.CommandContext(ctx, "tmux", append([]string{"-L", socket}, args...)...).CombinedOutput()
		if ctx.Err() != nil {
			return out, ctx.Err()
		}
		return out, err
	}

	out, probeErr := run("has-session", "-t", target)
	missing := probeErr != nil
	if missing && !missingTmuxSession(string(out)) {
		return nil, fmt.Errorf("check session %q: %w: %s", inst.Title, probeErr, strings.TrimSpace(string(out)))
	}
	dead := false
	if !missing {
		out, err = run("display-message", "-p", "-t", paneTarget, "#{pane_dead}")
		if err != nil {
			return nil, fmt.Errorf("check pane %q: %w: %s", inst.Title, err, strings.TrimSpace(string(out)))
		}
		dead = strings.TrimSpace(string(out)) == "1"
		if !dead && strings.TrimSpace(string(out)) != "0" {
			return nil, fmt.Errorf("could not determine pane state for session %q", inst.Title)
		}
	}
	if missing || dead {
		if virt == nil {
			virt = harness.NewVirtualizer("", "")
		}
		cfg, err := ReadSessionConfig(inst)
		if err != nil {
			return nil, err
		}
		virt, err = s.AgentVirtualizer(virt, cfg.AgentID)
		if err != nil {
			return nil, err
		}
		command, env, err := recoveryCommand(virt, inst)
		if err != nil {
			return nil, err
		}
		if virt == nil {
			virt = harness.NewVirtualizer("", "")
		}
		if err := s.BindAgentMemory(virt, inst); err != nil {
			return nil, err
		}
		args := []string{"new-session", "-d", "-s", name, "-c", inst.ProjectPath}
		if dead {
			// No -k: if another launcher already revived the pane, do not kill it.
			args = []string{"respawn-pane", "-t", paneTarget, "-c", inst.ProjectPath}
		}
		keys := make([]string, 0, len(env))
		for key := range env {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			args = append(args, "-e", key+"="+env[key])
		}
		args = append(args, command)
		if out, err = run(args...); err != nil {
			return nil, fmt.Errorf("recover session %q: %w: %s", inst.Title, err, strings.TrimSpace(string(out)))
		}
		// A command that exits immediately must not be reported as recovered.
		out, err = run("display-message", "-p", "-t", paneTarget, "#{pane_dead}")
		if err != nil || strings.TrimSpace(string(out)) != "0" {
			return nil, fmt.Errorf("session %q exited during recovery; check its saved command %q", inst.Title, inst.Command)
		}
		if _, err := s.db.Exec(`UPDATE instances SET status = 'running' WHERE id = ?`, inst.ID); err != nil {
			return nil, fmt.Errorf("save recovered session status: %w", err)
		}
	}
	// Protect hotkey usage: ensure root C-q detaches client cleanly without needing
	// prefix C-b (which outer tmux intercepts), and offer C-a as secondary prefix.
	_, _ = run("bind-key", "-n", "C-q", "detach-client")
	_, _ = run("set-option", "-t", name, "prefix2", "C-a")

	if _, err := s.db.Exec(`UPDATE instances SET tmux_session = ?, last_accessed = ? WHERE id = ?`, name, time.Now().Unix(), inst.ID); err != nil {
		return nil, fmt.Errorf("save recovered session: %w", err)
	}
	InvalidateInspectionCache("")
	return TmuxAttachCommand(name, socket), nil
}

func missingTmuxSession(output string) bool {
	return strings.Contains(output, "can't find session:") ||
		strings.Contains(output, "no server running on ") ||
		(strings.Contains(output, "error connecting to ") && strings.Contains(output, "No such file or directory"))
}

// TmuxAttachCommand also works when ai-desk itself is running inside tmux.
// An explicit socket keeps clearing TMUX from changing the target server.
func TmuxAttachCommand(name, socket string) *exec.Cmd {
	args := []string{}
	if socket != "" {
		args = append(args, "-L", socket)
	} else if inherited := os.Getenv("TMUX"); inherited != "" {
		if path, _, ok := strings.Cut(inherited, ","); ok {
			args = append(args, "-S", path)
		}
	}
	args = append(args, "attach-session")
	if name != "" {
		args = append(args, "-t", "="+name)
	}
	cmd := exec.Command("tmux", args...)
	for _, env := range os.Environ() {
		if !strings.HasPrefix(env, "TMUX=") && !strings.HasPrefix(env, "TMUX_PANE=") {
			cmd.Env = append(cmd.Env, env)
		}
	}
	return cmd
}

func recoveryCommand(virt *harness.Virtualizer, inst *InstanceRecord) (string, map[string]string, error) {
	var metadata map[string]interface{}
	if inst.ToolData != "" {
		if err := json.Unmarshal([]byte(inst.ToolData), &metadata); err != nil {
			return "", nil, fmt.Errorf("read session recovery metadata: %w", err)
		}
	}
	command := strings.TrimSpace(inst.Command)
	if command == "" {
		if inst.Tool == "shell" || inst.Tool == "" {
			command = os.Getenv("SHELL")
			if command == "" {
				command = "/bin/sh"
			}
			command = shellQuote(command)
		} else {
			_, normalized, err := NormalizeHarnessName(inst.Tool)
			if err != nil {
				return "", nil, err
			}
			command = normalized
		}
	}
	// Only augment a bare harness command. Custom commands may already contain
	// resume arguments, prompts, or shell syntax and must retain their meaning.
	if command == inst.Tool {
		isFresh, _ := metadata["is_fresh_harness"].(bool)
		idKey, resumeFlag := "", ""
		switch inst.Tool {
		case "codex":
			idKey, resumeFlag = "codex_session_id", " resume "
		case "claude":
			idKey, resumeFlag = "claude_session_id", " --resume "
		case "opencode":
			idKey, resumeFlag = "opencode_session_id", " --session "
		case "agy":
			// Agy stores its conversations in the account sandbox and resolves
			// --continue against the restored working directory.
			// Only continue if not a fresh harness switch.
			if !isFresh {
				command += " --continue"
			}
		case "gemini":
			idKey, resumeFlag = "gemini_session_id", " --resume "
		}
		if id, _ := metadata[idKey].(string); id != "" && !isFresh {
			command += resumeFlag + shellQuote(id)
		}
	}
	if inst.Wrapper != "" {
		if strings.Contains(inst.Wrapper, "{command}") {
			command = strings.ReplaceAll(inst.Wrapper, "{command}", command)
		} else {
			command = inst.Wrapper + " " + command
		}
	}
	var env map[string]string
	if metadata["created_by"] == "ai-desk" {
		if virt == nil {
			virt = harness.NewVirtualizer("", "")
		}
		account := inst.Account
		if account == "" {
			account = "default"
		}
		skills, err := SessionSkills(virt, inst)
		virt = SessionVirtualizer(virt, inst)
		if err != nil {
			return "", nil, fmt.Errorf("restore session skills: %w", err)
		}
		if err := virt.PrepareCredentials(inst.Tool, account); err != nil {
			return "", nil, err
		}
		env, err = virt.PrepareSandboxWithSkills(inst.Tool, account, skills)
		if err != nil {
			return "", nil, fmt.Errorf("restore session account: %w", err)
		}
	}
	return command, env, nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
