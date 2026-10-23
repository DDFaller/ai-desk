package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
)

func isRateLimit(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "http 429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "resource has been exhausted") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "usage limit exceeded") ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota exceeded")
}

type captureBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (b *captureBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	n := len(p)
	if len(p) > 131072 {
		p = p[len(p)-131072:]
	}
	if b.b.Len()+len(p) > 131072 {
		b.b.Reset()
	}
	_, err := b.b.Write(p)
	return n, err
}
func (b *captureBuffer) String() string { b.mu.Lock(); defer b.mu.Unlock(); return b.b.String() }

func runCommand(tool string, args []string, env map[string]string) (int, string, error) {
	cmd := exec.Command(tool, args...)
	// Inherit existing process environment and apply overrides
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var outputBuf captureBuffer
	multiOut := io.MultiWriter(os.Stdout, &outputBuf)
	multiErr := io.MultiWriter(os.Stderr, &outputBuf)

	cmd.Stdout = multiOut
	cmd.Stderr = multiErr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		exitCode = 1
	}
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			exitCode = status.ExitStatus()
		}
	}
	return exitCode, outputBuf.String(), err
}

func main() {
	account := flag.String("profile", "default", "Legacy account ID (use -account-profile for routing policy)")
	profile := flag.String("account-profile", "", "Account routing profile")
	skills := flag.String("skills", "none", "Skill profile; none starts without skills")
	provider := flag.String("provider", "", "Custom provider")
	noResume := flag.Bool("no-resume", false, "Disable automatic failover")
	flag.Parse()
	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}
	tool, _, err := deck.NormalizeHarnessName(args[0])
	if err != nil {
		fatal(err)
	}
	profiles, err := rotation.LoadAccountProfiles("")
	if err != nil {
		fatal(err)
	}
	if *profile == "" {
		*profile = profiles.ActiveProfile
	}
	policy, ok := profiles.Profiles[*profile]
	if !ok {
		fatal(fmt.Errorf("unknown account profile %q", *profile))
	}
	cfg, err := harness.LoadSkillProfiles("")
	if err != nil {
		fatal(err)
	}
	v := harness.NewVirtualizer("", "").ForSession(fmt.Sprintf("shim-%d-%d", os.Getpid(), time.Now().UnixNano()))
	selected, err := cfg.ResolveSkills(*skills, v.SkillsPath())
	if err != nil {
		fatal(err)
	}
	current, err := deck.RouteAccount(tool, *profile, *account)
	if err != nil && *provider == "" {
		fatal(err)
	}
	var env map[string]string
	if *provider != "" {
		registry, err := providers.Load("")
		if err != nil {
			fatal(err)
		}
		p, ok := registry.Get(*provider)
		if !ok {
			fatal(fmt.Errorf("unknown provider %q", *provider))
		}
		env, err = v.PrepareSandboxWithSkills(tool, "default", selected)
		if err != nil {
			fatal(err)
		}
		for k, value := range providers.NewTranslator().EnvOverrides(tool, p, 0) {
			env[k] = value
		}
	} else {
		if err = v.PrepareCredentials(tool, current); err != nil {
			fatal(err)
		}
		env, err = v.PrepareSandboxWithSkills(tool, current, selected)
		if err != nil {
			fatal(err)
		}
	}
	fmt.Fprintf(os.Stderr, "[AI-DESK] %s: account=%s profile=%s skills=%s (%d)\n", tool, current, *profile, *skills, len(selected))
	code, out, runErr := runCommand(tool, args[1:], env)
	if code != 0 && isRateLimit(out) && policy.AutoFailover && !*noResume && *provider == "" && current != "default" {
		accounts, err := harness.ListAccounts(tool)
		if err != nil {
			fatal(err)
		}
		candidates := profiles.FilterAccounts(*profile, accounts)
		next, err := rotation.FailoverPersisted(candidates, current, 20*time.Minute, policy.Policy, tool)
		if err != nil {
			fatal(err)
		}
		if err = v.PrepareCredentials(tool, next.ID); err != nil {
			fatal(err)
		}
		nextEnv, err := v.PrepareSandboxWithSkills(tool, next.ID, selected)
		if err != nil {
			fatal(err)
		}
		// Explicitly preserve the invocation. Do not invent a universal --resume flag.
		fmt.Fprintf(os.Stderr, "[AI-DESK] Switched to %s; retrying the original invocation.\n", next.ID)
		code, _, runErr = runCommand(tool, args[1:], nextEnv)
	}
	if runErr != nil && code == 0 {
		fatal(runErr)
	}
	if code != 0 {
		os.Exit(code)
	}
}

func fatal(err error) { fmt.Fprintln(os.Stderr, "[AI-DESK]", err); os.Exit(1) }
