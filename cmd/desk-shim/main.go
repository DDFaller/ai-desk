package main

import (
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"time"

	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

func isRateLimit(output string) bool {
	lower := strings.ToLower(output)
	return strings.Contains(lower, "429") ||
		strings.Contains(lower, "rate limit") ||
		strings.Contains(lower, "resource has been exhausted") ||
		strings.Contains(lower, "resource_exhausted") ||
		strings.Contains(lower, "usage limit exceeded") ||
		strings.Contains(lower, "insufficient_quota") ||
		strings.Contains(lower, "quota exceeded")
}

func runCommand(tool string, args []string, env map[string]string) (int, string, error) {
	cmd := exec.Command(tool, args...)
	// Inherit existing process environment and apply overrides
	cmd.Env = os.Environ()
	for k, v := range env {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	var outputBuf bytes.Buffer
	multiOut := io.MultiWriter(os.Stdout, &outputBuf)
	multiErr := io.MultiWriter(os.Stderr, &outputBuf)

	cmd.Stdout = multiOut
	cmd.Stderr = multiErr
	cmd.Stdin = os.Stdin

	err := cmd.Run()
	exitCode := 0
	if cmd.ProcessState != nil {
		if status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); ok {
			exitCode = status.ExitStatus()
		}
	}
	return exitCode, outputBuf.String(), err
}

func main() {
	profileFlag := flag.String("profile", "default", "Account or provider profile")
	providerFlag := flag.String("provider", "", "Custom provider name from providers.yaml")
	noResumeFlag := flag.Bool("no-resume", false, "Disable automatic --resume on rate limit failover")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		fmt.Println("Usage: desk-shim [options] <tool> [tool args...]")
		fmt.Println("Options:")
		flag.PrintDefaults()
		os.Exit(1)
	}

	tool := args[0]
	toolArgs := args[1:]

	v := harness.NewVirtualizer("", "")
	sb := swapbridge.NewSwapBridge("")
	reg, _ := providers.Load("")
	translator := providers.NewTranslator()

	// Load accounts
	accounts, _ := sb.LoadAccounts()
	engine := rotation.NewEngine(nil)

	// Pick initial account/provider
	currentID := *profileFlag
	var currentEnv map[string]string
	var err error

	if *providerFlag != "" {
		if p, ok := reg.Get(*providerFlag); ok {
			currentEnv = translator.EnvOverrides(tool, p, 0)
			currentID = p.Name
		}
	} else if len(accounts) > 0 {
		selected, selErr := engine.SelectNext(accounts, rotation.PolicySticky, currentID)
		if selErr == nil {
			currentID = selected.ID
			currentEnv, err = v.PrepareSandbox(tool, currentID)
			if err != nil {
				fmt.Fprintf(os.Stderr, "[AI-DESK] Warning: prepare sandbox: %v\n", err)
			}
			token, _ := sb.ReadAccountToken(selected.Email)
			_ = v.WriteAgyToken(currentID, token, selected.Email)
		}
	}

	fmt.Fprintf(os.Stderr, "[AI-DESK] Launching %s in session sandbox [%s]...\n", tool, currentID)

	code, out, runErr := runCommand(tool, toolArgs, currentEnv)

	// Check if turn hit a rate limit
	if isRateLimit(out) && len(accounts) > 1 && !*noResumeFlag {
		fmt.Fprintf(os.Stderr, "\n\033[1;33m[AI-DESK] Rate limit / quota exhaustion detected on account %s!\033[0m\n", currentID)
		nextAcc, failErr := engine.Failover(accounts, currentID, 30*time.Minute, rotation.PolicyBalanced)
		if failErr == nil {
			fmt.Fprintf(os.Stderr, "\033[1;32m[AI-DESK] Seamlessly rotating to next healthy account: %s (remaining: %.1f%%)\033[0m\n", nextAcc.Email, nextAcc.RemainingQuotaPct)
			newEnv, _ := v.PrepareSandbox(tool, nextAcc.ID)
			token, _ := sb.ReadAccountToken(nextAcc.Email)
			_ = v.WriteAgyToken(nextAcc.ID, token, nextAcc.Email)

			// Add --resume flag if not already present
			hasResume := false
			for _, a := range toolArgs {
				if a == "--resume" || a == "-c" || a == "-r" {
					hasResume = true
					break
				}
			}
			resumeArgs := toolArgs
			if !hasResume {
				resumeArgs = append(resumeArgs, "--resume")
			}

			fmt.Fprintf(os.Stderr, "[AI-DESK] Transparently resuming session with new credentials...\n\n")
			code, _, runErr = runCommand(tool, resumeArgs, newEnv)
		} else {
			fmt.Fprintf(os.Stderr, "[AI-DESK] Failover failed: %v\n", failErr)
		}
	}

	if runErr != nil && code != 0 {
		os.Exit(code)
	}
}
