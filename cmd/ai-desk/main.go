package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/swapbridge"
	"github.com/falluba/ai-desk/pkg/ui"
)

const version = "0.1.0"

func printHelp() {
	fmt.Printf("AI Desk v%s - Unified AI Command Center\n\n", version)
	fmt.Println("Usage: ai-desk [command] [options] [arguments...]")
	fmt.Println()
	fmt.Println("Running 'ai-desk' with no arguments launches the interactive TUI.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  tui, ui             Launch the interactive terminal user interface")
	fmt.Println("  status              Show all saved accounts, quota meters, providers, and sessions")
	fmt.Println("  run [opts] <tool>   Launch an AI CLI with scoped skills & auto-rotation")
	fmt.Println("                      Options: --skills <profile> (minimal, core, web, video, career, all)")
	fmt.Println("  skills [action]     Manage skill profiles & conscious runtime attachment")
	fmt.Println("                      Actions: list, attach <tool> <acc> <skill>, detach <tool> <acc> <skill>")
	fmt.Println("  session [action]    Manage tmux agent sessions (list, attach, new)")
	fmt.Println("  providers [list]    Manage and inspect custom LLM providers (Ollama, OpenRouter, etc.)")
	fmt.Println("  sync                Fetch upstream updates for agent-deck and agy-swap submodules")
	fmt.Println("  version             Display version and system information")
	fmt.Println("  help                Show this help message")
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
		fmt.Println("Usage: ai-desk run [--skills <profile>] <tool> [args...]")
		os.Exit(1)
	}

	// Parse optional --skills flag
	shimArgs := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		if args[i] == "--skills" && i+1 < len(args) {
			shimArgs = append(shimArgs, "-skills", args[i+1])
			i++
		} else {
			shimArgs = append(shimArgs, args[i])
		}
	}

	shimPath, err := exec.LookPath("desk-shim")
	if err != nil {
		shimPath = "./bin/desk-shim"
	}

	cmd := exec.Command(shimPath, shimArgs...)
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

func cmdSkills(args []string) {
	sub := "list"
	if len(args) > 0 {
		sub = args[0]
	}

	v := harness.NewVirtualizer("", "")
	cfg, err := harness.LoadSkillProfiles("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading skill profiles: %v\n", err)
		os.Exit(1)
	}

	switch sub {
	case "list":
		fmt.Println("\n\033[1;36m=== AI Desk Skill Profiles (~/.ai-desk/skill-profiles.yaml) ===\033[0m")
		for name, p := range cfg.Profiles {
			skills, _ := cfg.ResolveSkills(name, "")
			count := len(skills)
			estTokens := count * 60
			if count == 76 {
				estTokens = 4500
			}
			fmt.Printf("\n\033[1m[%s]\033[0m  %s\n", name, p.Description)
			fmt.Printf("  • Skills: %d skills (~%d prompt tokens/turn)\n", count, estTokens)
			if count <= 15 {
				fmt.Printf("  • Included: %s\n", strings.Join(skills, ", "))
			} else {
				fmt.Printf("  • Included: %s, ... (%d more)\n", strings.Join(skills[:10], ", "), count-10)
			}
		}
		fmt.Println()

	case "attach":
		if len(args) < 4 {
			fmt.Println("Usage: ai-desk skills attach <tool> <account_id> <skill_name>")
			fmt.Println("Example: ai-desk skills attach claude default shadcn")
			return
		}
		tool := args[1]
		acc := args[2]
		skill := args[3]
		if err := v.AttachSkill(tool, acc, skill); err != nil {
			fmt.Printf("\033[31mError attaching skill:\033[0m %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Skill %q attached successfully to %s/%s sandbox!\033[0m\n", skill, tool, acc)
		fmt.Printf("  Location: %s/skills/%s\n", v.SandboxPath(tool, acc), skill)

	case "detach":
		if len(args) < 4 {
			fmt.Println("Usage: ai-desk skills detach <tool> <account_id> <skill_name>")
			return
		}
		tool := args[1]
		acc := args[2]
		skill := args[3]
		if err := v.DetachSkill(tool, acc, skill); err != nil {
			fmt.Printf("\033[31mError detaching skill:\033[0m %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Skill %q detached from %s/%s sandbox.\033[0m\n", skill, tool, acc)
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

func runTUI() {
	p := tea.NewProgram(ui.NewModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error running TUI: %v\n", err)
		os.Exit(1)
	}
}

func main() {
	if len(os.Args) < 2 {
		runTUI()
		os.Exit(0)
	}

	command := os.Args[1]
	subArgs := os.Args[2:]

	switch command {
	case "tui", "ui":
		runTUI()
	case "status":
		cmdStatus()
	case "run":
		cmdRun(subArgs)
	case "skills":
		cmdSkills(subArgs)
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
