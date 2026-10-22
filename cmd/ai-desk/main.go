package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/swapbridge"
)

const version = "0.1.0"

func printHelp() {
	fmt.Printf("AI Desk v%s - Unified AI Command Center\n\n", version)
	fmt.Println("Usage: ai-desk <command> [options] [arguments...]")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  status              Show all saved accounts, quota meters, providers, and sessions")
	fmt.Println("  run <tool> [args]   Launch an AI CLI wrapped with per-session sandboxing & auto-rotation")
	fmt.Println("  session [action]    Manage tmux agent sessions (list, attach, new)")
	fmt.Println("  providers [list]    Manage and inspect custom LLM providers (Ollama, OpenRouter, etc.)")
	fmt.Println("  sync                Fetch upstream updates for agent-deck and agy-swap submodules")
	fmt.Println("  version             Display version and system information")
	fmt.Println()
}

func cmdStatus() {
	sb := swapbridge.NewSwapBridge("")
	accounts, err := sb.LoadAccounts()

	fmt.Println("\n\033[1;36m=== AI Desk Status ===\033[0m")
	fmt.Println("\n\033[1mGoogle Antigravity Accounts (from agy-swap vault):\033[0m")
	if err != nil || len(accounts) == 0 {
		fmt.Println("  No accounts found or unable to read ~/.gemini/agy-swap/accounts.json")
	} else {
		for i, a := range accounts {
			bar := renderProgressBar(a.RemainingQuotaPct)
			fmt.Printf("  [%d] %-30s  %s  %5.1f%%\n", i+1, a.Email, bar, a.RemainingQuotaPct)
		}
	}

	reg, regErr := providers.Load("")
	fmt.Println("\n\033[1mCustom Providers (~/.ai-desk/providers.yaml):\033[0m")
	if regErr != nil || len(reg.Providers) == 0 {
		fmt.Println("  No custom providers configured. Add one with `ai-desk providers add` or edit ~/.ai-desk/providers.yaml")
	} else {
		for name, p := range reg.Providers {
			keyCount := len(p.APIKeys)
			if keyCount == 0 && p.APIKey != "" {
				keyCount = 1
			}
			fmt.Printf("  • %-16s type=%-18s endpoint=%-32s (keys=%d)\n", name, p.Type, p.BaseURL, keyCount)
		}
	}

	engine := rotation.NewEngine(nil)
	cooldowns := engine.Ledger().ActiveCooldowns()
	if len(cooldowns) > 0 {
		fmt.Println("\n\033[1;33mActive Cooldowns:\033[0m")
		for id, rec := range cooldowns {
			fmt.Printf("  ⚠ %s in cooldown until %s (reason: %s)\n", id, rec.Until.Format("15:04:05"), rec.Reason)
		}
	}

	fmt.Println("\n\033[1mActive Tmux Sessions:\033[0m")
	out, tmuxErr := exec.Command("tmux", "list-sessions").Output()
	if tmuxErr != nil || len(out) == 0 {
		fmt.Println("  No active tmux sessions running.")
	} else {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			fmt.Printf("  • %s\n", line)
		}
	}
	fmt.Println()
}

func renderProgressBar(pct float64) string {
	width := 20
	filled := int((pct / 100.0) * float64(width))
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	bar := strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
	if pct > 40 {
		return fmt.Sprintf("\033[32m%s\033[0m", bar) // Green
	} else if pct > 15 {
		return fmt.Sprintf("\033[33m%s\033[0m", bar) // Yellow
	}
	return fmt.Sprintf("\033[31m%s\033[0m", bar) // Red
}

func cmdRun(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ai-desk run <tool> [args...]")
		os.Exit(1)
	}

	// Delegate to desk-shim
	shimPath, err := exec.LookPath("desk-shim")
	if err != nil {
		// Look in current binary directory or ./bin/desk-shim
		shimPath = "./bin/desk-shim"
	}

	cmd := exec.Command(shimPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}

func cmdProviders(args []string) {
	reg, err := providers.Load("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading providers: %v\n", err)
		os.Exit(1)
	}

	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}

	switch sub {
	case "list":
		fmt.Println("\n\033[1mConfigured Providers:\033[0m")
		if len(reg.Providers) == 0 {
			fmt.Println("  None. Config file:", providers.DefaultConfigPath())
			return
		}
		for name, p := range reg.Providers {
			fmt.Printf("\n[%s]\n", name)
			fmt.Printf("  Type:     %s\n", p.Type)
			fmt.Printf("  Base URL: %s\n", p.BaseURL)
			if len(p.Models) > 0 {
				fmt.Printf("  Models:   %s\n", strings.Join(p.Models, ", "))
			}
		}
	case "test":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk providers test <name>")
			return
		}
		name := args[1]
		p, ok := reg.Get(name)
		if !ok {
			fmt.Printf("Provider %q not found in providers.yaml\n", name)
			return
		}
		fmt.Printf("Pinging endpoint %s ... ", p.BaseURL)
		client := http.Client{Timeout: 3 * time.Second}
		resp, err := client.Get(p.BaseURL)
		if err != nil {
			fmt.Printf("\033[31mFAILED\033[0m (%v)\n", err)
		} else {
			resp.Body.Close()
			fmt.Printf("\033[32mOK\033[0m (HTTP %s)\n", resp.Status)
		}
	}
}

func cmdSession(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}
	switch sub {
	case "list":
		out, err := exec.Command("tmux", "list-sessions").CombinedOutput()
		if err != nil {
			fmt.Println("No active tmux sessions.")
			return
		}
		fmt.Println(string(out))
	case "attach":
		target := ""
		if len(args) > 1 {
			target = args[1]
		}
		tmuxArgs := []string{"attach"}
		if target != "" {
			tmuxArgs = append(tmuxArgs, "-t", target)
		}
		cmd := exec.Command("tmux", tmuxArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	}
}

func cmdSync() {
	fmt.Println("Running upstream submodule sync...")
	cmd := exec.Command("./scripts/sync-upstream.sh")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Sync failed: %v\n", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		printHelp()
		os.Exit(0)
	}

	command := os.Args[1]
	subArgs := os.Args[2:]

	switch command {
	case "status":
		cmdStatus()
	case "run":
		cmdRun(subArgs)
	case "session":
		cmdSession(subArgs)
	case "providers":
		cmdProviders(subArgs)
	case "sync":
		cmdSync()
	case "version", "--version", "-v":
		fmt.Printf("ai-desk version %s\n", version)
	case "help", "--help", "-h":
		printHelp()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printHelp()
		os.Exit(1)
	}
}
