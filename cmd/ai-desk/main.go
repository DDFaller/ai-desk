package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"strings"
	"syscall"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/assistant"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/routines"
	"github.com/falluba/ai-desk/pkg/rtk"
	"github.com/falluba/ai-desk/pkg/swapbridge"
	"github.com/falluba/ai-desk/pkg/ui"
	"github.com/falluba/ai-desk/pkg/voice"
)

const version = ui.Version

func printHelp() {
	fmt.Printf("AI Desk v%s - Unified AI Command Center\n\n", version)
	fmt.Println("Usage: ai-desk [command] [options] [arguments...]")
	fmt.Println()
	fmt.Println("Running 'ai-desk' with no arguments launches the interactive TUI.")
	fmt.Println()
	fmt.Println("Commands:")
	fmt.Println("  tui, ui             Launch the interactive terminal user interface")
	fmt.Println("  status              Show all saved accounts, quota meters, providers, and sessions")
	fmt.Println("  accounts [list]     Inspect managed accounts & active task allocation")
	fmt.Println("  task create [opts]  Create & launch a new isolated task (or press [n] in TUI)")
	fmt.Println("  routine [action]    Manage scheduled cron routines & recurring background agents")
	fmt.Println("                      Actions: list, add, run <id>, pause <id>, resume <id>, delete <id>, daemon")
	fmt.Println("  voice [action]      Resident local voice models & bidirectional agent voice bridge")
	fmt.Println("                      Actions: chat [session], attach <session>, status, start, hear, speak <text>, daemon")
	fmt.Println("  run [opts] <tool>   Launch an AI CLI with scoped skills & auto-rotation")
	fmt.Println("                      Options: --skills <profile> (auto, minimal, core, web, video, career, all)")
	fmt.Println("  skills [action]     Manage skill profiles & conscious runtime attachment")
	fmt.Println("                      Actions: list, audit, attach <tool> <acc> <skill>, detach <tool> <acc> <skill>")
	fmt.Println("  rtk [action]        Inspect RTK token savings dashboard or initialize harness hooks")
	fmt.Println("                      Actions: gain (default), init, config")
	fmt.Println("  group [action]      Manage hierarchical groups & subgroups")
	fmt.Println("                      Actions: list, create <path>, delete <path> -r, move <session> <group>")
	fmt.Println("  profile [action]    Manage account profiles & auto-failover policies")
	fmt.Println("                      Actions: list, switch <name>, assign <profile> <email>")
	fmt.Println("  session [action]    Manage agent sessions (list, create, attach, inspect, switch, account)")
	fmt.Println("                      Actions: list, create <title>, attach <name>, inspect <name>, switch <id> <tool>, account <id> <acc>")
	fmt.Println("  stack <url>         Reverse-engineer SaaS tech stack in 10s (headers, frontend, analytics, auth)")
	fmt.Println("  assist, auto        Resident Virtual Assistant (Grok-bot mode: session monitor & 429 failover)")
	fmt.Println("                      Actions: status (briefing), daemon (background monitor), failover <session-id>")
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

	rtkClient := rtk.NewClient()
	fmt.Println("\n\033[1mRTK Token Saver (Rust Token Killer):\033[0m")
	if rtkClient.IsInstalled() {
		ver, _ := rtkClient.Version()
		savings, sErr := rtkClient.GetSavings()
		if sErr == nil {
			fmt.Printf("  • Status:       \033[1;32mActive\033[0m (%s)\n", ver)
			fmt.Printf("  • Interceptions: %d commands\n", savings.TotalCommands)
			fmt.Printf("  • Tokens Saved:  \033[1;36m%d tokens\033[0m (saved %.1f%% of raw terminal output)\n",
				savings.TotalSaved, savings.AvgSavingsPct*100)
		} else {
			fmt.Printf("  • Status:       \033[1;32mActive\033[0m (%s)\n", ver)
		}
	} else {
		fmt.Println("  • Status: \033[1;33mNot Installed\033[0m (Run `make install-rtk` to install RTK for 60-90% token reduction)")
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
			if count >= 70 {
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
		fmt.Println("\nTip: Use 'ai-desk skills show <profile>' to list all skills in a profile.")
		fmt.Println()

	case "show":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk skills show <profile>")
			fmt.Println("Example: ai-desk skills show core")
			return
		}
		pName := args[1]
		skills, err := cfg.ResolveSkills(pName, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return
		}
		p := cfg.Profiles[pName]
		count := len(skills)
		estTokens := count * 60
		if count >= 70 {
			estTokens = 4500
		}
		fmt.Printf("\n\033[1;36m=== Profile: %s (%s) ===\033[0m\n", pName, p.Description)
		fmt.Printf("Total Scoped Skills: %d skills (~%d prompt tokens/turn)\n\n", count, estTokens)
		for _, s := range skills {
			fmt.Printf("  • %s\n", s)
		}
		fmt.Println()

	case "catalog", "all":
		skills, err := harness.ListAllSkillNames("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error listing skills: %v\n", err)
			return
		}
		fmt.Println("\n\033[1;36m=== AI Desk Canonical Skills Catalog (~/.agents/skills) ===\033[0m")
		fmt.Printf("Total Master Skills: %d skills\n\n", len(skills))
		for i, s := range skills {
			fmt.Printf("  %2d. %-32s", i+1, s)
			if (i+1)%2 == 0 || i == len(skills)-1 {
				fmt.Println()
			}
		}
		fmt.Println("\nTip: Scope sessions with --skills <profile> to save up to 95% prompt tokens.")
		fmt.Println()

	case "audit":
		fmt.Println("\n\033[1;36m=== AI Desk Skill Catalog Audit ===\033[0m")
		report, err := harness.AuditSkills("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Audit error: %v\n", err)
			return
		}
		fmt.Printf("Total Available Skills:  \033[1m%d skills\033[0m\n", report.TotalSkills)
		fmt.Printf("Total Full Skill Words:  \033[1m%d words\033[0m (~%d tokens)\n", report.TotalWords, int(float64(report.TotalWords)*1.33))
		fmt.Printf("Prompt Catalog Tax:      \033[1;31m~%d tokens/turn\033[0m (when unscoped)\n\n", report.CatalogTokenTax)

		fmt.Println("\033[1mSkills Distribution by Domain:\033[0m")
		for cat, count := range report.CategoryCounts {
			pct := float64(count) / float64(report.TotalSkills) * 100.0
			fmt.Printf("  • %-10s  %2d skills  (%4.1f%%)\n", cat, count, pct)
		}

		fmt.Println("\n\033[1mScoping Optimization Potential:\033[0m")
		fmt.Println("  • minimal:  ~120 tokens/turn  (saves 97% prompt overhead)")
		fmt.Println("  • core:     ~240 tokens/turn  (saves 94% prompt overhead)")
		fmt.Println("  • web:      ~960 tokens/turn  (saves 78% prompt overhead)")
		fmt.Println("  • video:    ~780 tokens/turn  (saves 82% prompt overhead)")
		fmt.Println("  • career:  ~1080 tokens/turn  (saves 76% prompt overhead)")
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

	default:
		// Check if argument is a known profile name (e.g. "ai-desk skills core")
		if _, ok := cfg.Profiles[sub]; ok {
			skills, _ := cfg.ResolveSkills(sub, "")
			p := cfg.Profiles[sub]
			count := len(skills)
			estTokens := count * 60
			if count >= 70 {
				estTokens = 4500
			}
			fmt.Printf("\n\033[1;36m=== Profile: %s (%s) ===\033[0m\n", sub, p.Description)
			fmt.Printf("Total Scoped Skills: %d skills (~%d prompt tokens/turn)\n\n", count, estTokens)
			for _, s := range skills {
				fmt.Printf("  • %s\n", s)
			}
			fmt.Println()
			return
		}
		fmt.Printf("Unknown skills action %q.\n\nUsage: ai-desk skills [list | show <profile> | catalog | audit | attach <tool> <acc> <skill> | detach <tool> <acc> <skill>]\n", sub)
	}
}

func cmdRTK(args []string) {
	rtkClient := rtk.NewClient()
	if !rtkClient.IsInstalled() {
		fmt.Fprintln(os.Stderr, "RTK (Rust Token Killer) is not installed.")
		fmt.Fprintln(os.Stderr, "Install it with: make install-rtk")
		return
	}

	action := "status"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "status", "gain":
		c := exec.Command(rtkClient.BinPath(), "gain")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		_ = c.Run()

	case "init":
		fmt.Println("Configuring RTK global hooks across AI harnesses (Claude, Gemini, Codex, OpenCode)...")
		if err := rtkClient.InitHarnessHooks(); err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing hooks: %v\n", err)
			return
		}
		fmt.Println("✓ RTK hooks initialized across all harnesses!")

	case "config":
		c := exec.Command(rtkClient.BinPath(), "config")
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		_ = c.Run()

	default:
		fmt.Printf("Usage: ai-desk rtk [gain|init|config]\n")
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
		if err := attachSession(target); err != nil {
			fmt.Fprintf(os.Stderr, "Could not attach to session: %v\n", err)
			os.Exit(1)
		}
	case "inspect", "info", "show":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk session inspect <id|title>")
			return
		}
		cmdSessionInspect(args[1])
	case "switch", "migrate":
		if len(args) < 3 {
			fmt.Println("Usage: ai-desk session switch <id|title> <target-tool> [options]")
			fmt.Println("Target tools: agy, claude, codex, opencode")
			return
		}
		cmdSessionSwitch(args[1:])
	case "account":
		if len(args) < 3 {
			fmt.Println("Usage: ai-desk session account <id|title> <target-account> [options]")
			fmt.Println("Options:")
			fmt.Println("  --no-respawn    Do not respawn pane process, only update environment")
			return
		}
		cmdSessionAccount(args[1:])
	case "create", "new":
		cmdTaskCreate(args[1:])
	default:
		fmt.Println("Usage: ai-desk session [list|create|attach|inspect|switch|account]")
	}
}

func cmdTaskCreate(args []string) {
	if len(args) == 0 {
		fmt.Println("Usage: ai-desk task create <title> [options]")
		fmt.Println()
		fmt.Println("Options:")
		fmt.Println("  --tool <agy|claude|codex|opencode|vox-studio>  Harness to launch (default: agy)")
		fmt.Println("  --group <path>                                Group path (default: my-sessions)")
		fmt.Println("  --path <dir>                                  Project working directory")
		fmt.Println("  --account <email>                             Account to isolate & route quota")
		fmt.Println("  --skills <profiles>                           Skill profile(s), comma-separated (e.g. core,web,video)")
		fmt.Println("  --prompt <text>                               Initial prompt to deliver")
		return
	}

	opts := deck.CreateTaskOptions{
		Title: args[0],
		Tool:  "agy",
	}

	for i := 1; i < len(args); i++ {
		switch args[i] {
		case "--tool":
			if i+1 < len(args) {
				opts.Tool = args[i+1]
				i++
			}
		case "--group":
			if i+1 < len(args) {
				opts.GroupPath = args[i+1]
				i++
			}
		case "--path":
			if i+1 < len(args) {
				opts.ProjectPath = args[i+1]
				i++
			}
		case "--account":
			if i+1 < len(args) {
				opts.Account = args[i+1]
				i++
			}
		case "--skills":
			if i+1 < len(args) {
				opts.SkillProfile = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				opts.InitialPrompt = args[i+1]
				i++
			}
		}
	}

	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	virt := harness.NewVirtualizer("", "")
	inst, err := sdb.CreateTask(virt, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError creating task: %v\033[0m\n", err)
		return
	}

	fmt.Printf("\n\033[32m✓ Task %q [%s] created successfully!\033[0m\n", inst.Title, inst.ID)
	fmt.Printf("  • Harness:       %s\n", inst.Tool)
	fmt.Printf("  • Group:         %s\n", inst.GroupPath)
	fmt.Printf("  • Project Path:  %s\n", inst.ProjectPath)
	fmt.Printf("  • Account:       %s\n", inst.Account)
	fmt.Printf("  • Tmux Session:  %s\n", inst.TmuxSession)
	fmt.Printf("  • Sandbox:       %s\n", virt.SandboxPath(inst.Tool, inst.Account))
	fmt.Println()
}

func cmdSessionSwitch(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: ai-desk session switch <id|title> <target-tool> [options]")
		fmt.Println()
		fmt.Println("Target Tools: agy, claude, codex, opencode")
		fmt.Println("Options:")
		fmt.Println("  --skills <profile>    Skill profile to project (e.g. core, web, minimal, all)")
		fmt.Println("  --fork / --new        Fork into a new task instance instead of recreating in-place")
		fmt.Println("  --no-handoff          Skip context handoff generation")
		fmt.Println("  --prompt <text>       Extra directive for target agent")
		fmt.Println("  --account <id>        Account ID or email for quota routing")
		return
	}

	sessionIDOrTitle := args[0]
	targetTool := args[1]

	opts := deck.SwitchOptions{
		TargetTool:     targetTool,
		IncludeHandoff: true,
	}

	for i := 2; i < len(args); i++ {
		switch args[i] {
		case "--no-handoff":
			opts.IncludeHandoff = false
		case "--fork", "--new":
			opts.CreateNewTask = true
		case "--skills":
			if i+1 < len(args) {
				opts.SkillProfile = args[i+1]
				i++
			}
		case "--prompt":
			if i+1 < len(args) {
				opts.CustomPrompt = args[i+1]
				i++
			}
		case "--account":
			if i+1 < len(args) {
				opts.AccountID = args[i+1]
				i++
			}
		}
	}

	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	virt := harness.NewVirtualizer("", "")

	fmt.Printf("Initiating cross-harness switch for session %q to %s...\n", sessionIDOrTitle, targetTool)
	res, err := deck.SwitchHarness(sdb, virt, sessionIDOrTitle, opts)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError switching harness: %v\033[0m\n", err)
		return
	}

	fmt.Printf("\n\033[32m✓ Session %q [%s] successfully switched from %s to %s!\033[0m\n",
		res.Instance.Title, res.Instance.ID, res.OldTool, res.NewTool)
	fmt.Printf("  • Working Dir:     %s\n", res.Instance.ProjectPath)
	fmt.Printf("  • Sandbox:         %s\n", res.SandboxPath)
	fmt.Printf("  • Skills Attached: %d\n", res.SkillsAttached)
	if res.HandoffPath != "" {
		fmt.Printf("  • Context Handoff: %s\n", res.HandoffPath)
		fmt.Printf("    %s\n", res.HandoffSummary)
	}
	if res.LaunchCommand != "" {
		fmt.Printf("  • Tmux Pane:       Respawned with command '%s'\n", res.LaunchCommand)
	}
	fmt.Printf("\nAttach to session with:\n  ai-desk session attach %s\n\n", res.Instance.Title)
}

func cmdSessionAccount(args []string) {
	if len(args) < 2 {
		fmt.Println("Usage: ai-desk session account <id|title> <target-account> [options]")
		fmt.Println("Options:")
		fmt.Println("  --no-respawn    Do not respawn pane process, only update environment")
		return
	}

	sessionIDOrTitle := args[0]
	targetAccount := args[1]
	respawn := true

	for i := 2; i < len(args); i++ {
		if args[i] == "--no-respawn" {
			respawn = false
		}
	}

	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	virt := harness.NewVirtualizer("", "")

	fmt.Printf("Switching account for session %q to %s...\n", sessionIDOrTitle, targetAccount)
	updated, err := deck.SwitchAccount(sdb, virt, sessionIDOrTitle, targetAccount, respawn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError switching account: %v\033[0m\n", err)
		return
	}

	fmt.Printf("\n\033[32m✓ Session %q [%s] account updated to %s!\033[0m\n",
		updated.Title, updated.ID, updated.Account)
	fmt.Printf("  • Harness:        %s\n", updated.Tool)
	fmt.Printf("  • Respawned Pane: %v\n", respawn)
	fmt.Printf("  • Sandbox:        %s\n", virt.SandboxPath(updated.Tool, updated.Account))
}

func cmdAccounts(args []string) {
	sb := swapbridge.NewSwapBridge("")
	accounts, err := sb.LoadAccounts()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading accounts: %v\n", err)
		return
	}

	sdb, _ := deck.OpenStateDB("")
	var instances []deck.InstanceRecord
	if sdb != nil {
		defer sdb.Close()
		instances, _ = sdb.LoadInstances()
	}

	fmt.Println("\n\033[1;36m=== Managed Accounts & Active Task Allocation ===\033[0m")
	for i, acc := range accounts {
		email := acc.Email
		if email == "" {
			email = acc.ID
		}
		bar := renderProgressBar(acc.RemainingQuotaPct)
		fmt.Printf("\n\033[1m[%d] %s\033[0m  Quota: %s %5.1f%%\n", i+1, email, bar, acc.RemainingQuotaPct)

		var bound []string
		for _, inst := range instances {
			if strings.EqualFold(inst.Account, email) || (inst.Account == "" && i == 0) {
				bound = append(bound, fmt.Sprintf("%s [%s]", inst.Title, inst.Tool))
			}
		}
		if len(bound) > 0 {
			fmt.Printf("    ↳ Active Tasks: %s\n", strings.Join(bound, ", "))
		} else {
			fmt.Println("    ↳ Active Tasks: (none)")
		}
	}
	fmt.Println()
}

func cmdSessionInspect(identifier string) {
	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	insp, err := sdb.InspectSession(identifier)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error inspecting session %q: %v\n", identifier, err)
		return
	}

	fmt.Printf("\n\033[1;36m=== AI Desk Session Inspector ===\033[0m\n\n")
	fmt.Printf("  \033[1mSession:\033[0m        %s (\033[90m%s\033[0m)\n", insp.Instance.Title, insp.Instance.ID)
	fmt.Printf("  \033[1mGroup:\033[0m          \033[1;34m%s\033[0m\n", insp.Instance.GroupPath)
	fmt.Printf("  \033[1mWorking Dir:\033[0m    %s\n", insp.Instance.ProjectPath)
	fmt.Printf("  \033[1mTool / Harness:\033[0m %s\n", insp.Instance.Tool)
	accStr := insp.ResolvedAccount.DisplayName
	if accStr == "" {
		accStr = insp.Instance.Account
	}
	if accStr == "" {
		accStr = "default"
	}
	fmt.Printf("  \033[1mAccount:\033[0m        \033[1;32m%s\033[0m\n", accStr)
	fmt.Printf("  \033[1mStatus:\033[0m         %s (Attached: %v)\n", insp.Instance.Status, insp.Attached)
	if insp.Uptime > 0 {
		fmt.Printf("  \033[1mUptime:\033[0m         %s\n", insp.Uptime.Round(time.Second))
	}
	if insp.Instance.TmuxSession != "" {
		fmt.Printf("  \033[1mTmux Session:\033[0m   %s\n", insp.Instance.TmuxSession)
	}

	fmt.Printf("\n  \033[1mActive Skills & Prompt Tax:\033[0m\n")
	fmt.Printf("    • Profile:    \033[1;32m%s\033[0m (%d active skills)\n", insp.ActiveProfile, len(insp.ActiveSkills))
	fmt.Printf("    • Prompt Tax: \033[1;33m~%d tokens/turn\033[0m\n", insp.PromptTaxTurn)
	if len(insp.ActiveSkills) > 0 {
		fmt.Printf("    • Skills:     %s\n", strings.Join(insp.ActiveSkills, ", "))
	}

	fmt.Printf("\n  \033[1mToken Telemetry:\033[0m\n")
	if insp.HasTokenMetrics {
		fmt.Printf("    • Total Tokens:  \033[1m%d\033[0m (Input: %d, Output: %d)\n",
			insp.TokensUsed.TotalTokens, insp.TokensUsed.InputTokens, insp.TokensUsed.OutputTokens)
		if insp.TokensUsed.CachedInputTokens > 0 {
			cachePct := float64(insp.TokensUsed.CachedInputTokens) / float64(insp.TokensUsed.InputTokens) * 100.0
			fmt.Printf("    • Cached Tokens: %d (%.1f%% cache hit)\n", insp.TokensUsed.CachedInputTokens, cachePct)
		}
		if insp.TokensUsed.ReasoningTokens > 0 {
			fmt.Printf("    • Reasoning Out: %d tokens\n", insp.TokensUsed.ReasoningTokens)
		}
	} else {
		fmt.Printf("    • No turn token metrics recorded yet.\n")
	}

	if insp.RTKSavings != nil && insp.RTKSavings.TotalCommands > 0 {
		fmt.Printf("    • RTK Savings:   %d tokens saved across %d commands (%.1f%% output reduction)\n",
			insp.RTKSavings.TotalSaved, insp.RTKSavings.TotalCommands, insp.RTKSavings.AvgSavingsPct*100.0)
	}

	if len(insp.TerminalPreview) > 0 {
		fmt.Printf("\n  \033[1mLive Terminal Capture (Last %d lines):\033[0m\n", len(insp.TerminalPreview))
		for _, l := range insp.TerminalPreview {
			fmt.Printf("    \033[90m│\033[0m %s\n", l)
		}
	}
	fmt.Println()
}

func cmdGroup(args []string) {
	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		groups, err := sdb.LoadGroups()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading groups: %v\n", err)
			return
		}
		instances, err := sdb.LoadInstances()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading instances: %v\n", err)
			return
		}

		roots := deck.BuildGroupTree(groups, instances)
		fmt.Printf("\n\033[1;36m=== AI Desk Group Hierarchy (%d groups, %d tasks) ===\033[0m\n\n", len(groups), len(instances))
		printGroupTreeCLI(roots, "")
		fmt.Println()

	case "create":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk group create <group/subgroup/path>")
			return
		}
		path := args[1]
		if err := sdb.CreateGroup(path); err != nil {
			fmt.Fprintf(os.Stderr, "Error creating group: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Group %q created successfully (including any missing parents)!\033[0m\n", path)

	case "delete", "del", "rm":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk group delete <group_path> [-r|--cascade]")
			return
		}
		path := args[1]
		cascade := false
		for _, a := range args[2:] {
			if a == "-r" || a == "--cascade" || a == "--force" || a == "-f" {
				cascade = true
			}
		}

		if !cascade {
			fmt.Printf("\033[1;33mWarning:\033[0m Deleting group %q without cascade flag.\n", path)
			fmt.Println("To delete this group AND all tasks/subgroups inside it, rerun with -r or --cascade:")
			fmt.Printf("  ai-desk group delete %q --cascade\n", path)
			return
		}

		deleted, err := sdb.DeleteGroupCascade(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting group: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Group %q and all %d associated sessions/subgroups deleted successfully!\033[0m\n", path, len(deleted))
		for _, d := range deleted {
			fmt.Printf("  • Terminated & removed session: %s [%s]\n", d.Title, d.ID)
		}

	case "move", "mv":
		if len(args) < 3 {
			fmt.Println("Usage: ai-desk group move <session_id_or_title> <target_group_path>")
			return
		}
		targetSess := args[1]
		targetGroup := args[2]
		if err := sdb.MoveSession(targetSess, targetGroup); err != nil {
			fmt.Fprintf(os.Stderr, "Error moving session: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Session %q moved to group %q successfully!\033[0m\n", targetSess, targetGroup)

	default:
		fmt.Println("Usage: ai-desk group [list|create|delete|move]")
	}
}

func printGroupTreeCLI(nodes []*deck.GroupNode, indent string) {
	for i, n := range nodes {
		isLast := i == len(nodes)-1
		marker := "├──"
		subIndent := indent + "│   "
		if isLast {
			marker = "└──"
			subIndent = indent + "    "
		}

		fmt.Printf("%s%s \033[1;34m%s\033[0m \033[90m(%d tasks)\033[0m\n", indent, marker, n.Name, n.TotalTasks)

		for _, s := range n.Sessions {
			statusColor := "\033[32m"
			if s.Status == "error" {
				statusColor = "\033[31m"
			} else if s.Status == "waiting" {
				statusColor = "\033[33m"
			}
			fmt.Printf("%s    • %-24s \033[90m[%s]\033[0m %s[%s]\033[0m\n", subIndent, s.Title, s.Tool, statusColor, s.Status)
		}

		if len(n.Children) > 0 {
			printGroupTreeCLI(n.Children, subIndent)
		}
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

func cmdRoutine(args []string) {
	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		routinesList, err := routines.LoadRoutines(sdb)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading routines: %v\n", err)
			return
		}
		fmt.Printf("\n\033[1;36m=== AI Desk Scheduled Routines & Recurring Agents (%d total) ===\033[0m\n\n", len(routinesList))
		if len(routinesList) == 0 {
			fmt.Println("  No routines registered yet. Add one with:")
			fmt.Println("    ai-desk routine add --title \"Daily Review\" --cron \"0 9 * * *\" --command \"agy task run triage\"")
			fmt.Println()
			return
		}

		for _, r := range routinesList {
			statusColor := "\033[32m" // green
			if r.Status == "paused" {
				statusColor = "\033[33m" // yellow
			}

			nextStr := "never"
			if r.NextRunAt > 0 {
				nextStr = time.Unix(r.NextRunAt, 0).Format("2006-01-02 15:04:05")
			}
			lastStr := "never"
			if r.LastRunAt > 0 {
				lastStr = fmt.Sprintf("%s (%s)", time.Unix(r.LastRunAt, 0).Format("15:04:05"), r.LastStatus)
			}

			fmt.Printf("  • \033[1m%-24s\033[0m \033[90m[%s]\033[0m %s[%s]\033[0m\n", r.Title, r.ID, statusColor, r.Status)
			fmt.Printf("      Schedule: \033[1;35m%-14s\033[0m Next: %-20s Last: %s\n", r.CronExpr, nextStr, lastStr)
			fmt.Printf("      Command:  \033[36m%s\033[0m (tool=%s acc=%s skills=%s)\n", r.Command, r.Tool, r.Account, r.Skills)
			if r.LastOutput != "" {
				preview := r.LastOutput
				if len(preview) > 80 {
					preview = preview[:77] + "..."
				}
				fmt.Printf("      Output:   \033[90m%s\033[0m\n", preview)
			}
			fmt.Println()
		}

	case "add":
		fs := flag.NewFlagSet("routine add", flag.ContinueOnError)
		title := fs.String("title", "", "Routine title / human label")
		cronExpr := fs.String("cron", "0 9 * * *", "5-field cron expression or alias (@daily, @hourly)")
		command := fs.String("command", "", "Command to execute on schedule")
		cmdAlias := fs.String("cmd", "", "Command alias")
		tool := fs.String("tool", "agy", "Agent tool harness")
		account := fs.String("account", "default", "Account to use")
		skills := fs.String("skills", "core", "Skill profile(s)")

		if err := fs.Parse(args[1:]); err != nil {
			return
		}

		finalCmd := *command
		if finalCmd == "" {
			finalCmd = *cmdAlias
		}
		remArgs := fs.Args()
		finalTitle := *title
		if finalTitle == "" && len(remArgs) > 0 {
			finalTitle = remArgs[0]
			remArgs = remArgs[1:]
		}
		if *cronExpr == "0 9 * * *" && len(remArgs) > 0 {
			*cronExpr = remArgs[0]
			remArgs = remArgs[1:]
		}
		if finalCmd == "" && len(remArgs) > 0 {
			finalCmd = strings.Join(remArgs, " ")
		}

		if finalTitle == "" {
			fmt.Println("Usage: ai-desk routine add --title <title> --cron <cron_expr> --command <cmd> [options]")
			return
		}
		if finalCmd == "" {
			fmt.Println("Error: --command cannot be empty")
			return
		}

		rec, err := routines.CreateRoutine(sdb, routines.RoutineRecord{
			Title:    finalTitle,
			CronExpr: *cronExpr,
			Command:  finalCmd,
			Tool:     *tool,
			Account:  *account,
			Skills:   *skills,
			Status:   "active",
		})
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error creating routine: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Created routine %q [%s]\033[0m\n", rec.Title, rec.ID)
		fmt.Printf("  Schedule: %s (next run: %s)\n", rec.CronExpr, time.Unix(rec.NextRunAt, 0).Format("2006-01-02 15:04:05"))

	case "run":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk routine run <id|title>")
			return
		}
		sched := routines.NewScheduler(sdb, 15*time.Second)
		r, err := sched.TriggerSync(args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error executing routine: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Executed routine %q [%s] (%s)!\033[0m\n", r.Title, r.ID, r.LastStatus)
		if r.LastOutput != "" {
			fmt.Printf("  Output: %s\n", r.LastOutput)
		}

	case "pause":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk routine pause <id|title>")
			return
		}
		r, err := routines.FindRoutine(sdb, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding routine: %v\n", err)
			return
		}
		if err := routines.UpdateRoutineStatus(sdb, r.ID, "paused"); err != nil {
			fmt.Fprintf(os.Stderr, "Error pausing routine: %v\n", err)
			return
		}
		fmt.Printf("\033[33m⏸ Paused routine %q [%s]\033[0m\n", r.Title, r.ID)

	case "resume":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk routine resume <id|title>")
			return
		}
		r, err := routines.FindRoutine(sdb, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding routine: %v\n", err)
			return
		}
		if err := routines.UpdateRoutineStatus(sdb, r.ID, "active"); err != nil {
			fmt.Fprintf(os.Stderr, "Error resuming routine: %v\n", err)
			return
		}
		fmt.Printf("\033[32m▶ Resumed routine %q [%s]\033[0m\n", r.Title, r.ID)

	case "delete", "rm", "del":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk routine delete <id|title>")
			return
		}
		r, err := routines.FindRoutine(sdb, args[1])
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error finding routine: %v\n", err)
			return
		}
		if err := routines.DeleteRoutine(sdb, r.ID); err != nil {
			fmt.Fprintf(os.Stderr, "Error deleting routine: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Deleted routine %q [%s]\033[0m\n", r.Title, r.ID)

	case "daemon":
		sched := routines.NewScheduler(sdb, 15*time.Second)
		sched.Start()
		fmt.Println("\n\033[1;36m=== AI Desk Routine Scheduler Daemon ===\033[0m")
		fmt.Println("Scheduler running in foreground (checking due routines every 15s).")
		fmt.Println("Press Ctrl+C to terminate.")

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		<-sigChan
		sched.Stop()
		fmt.Println("\nScheduler daemon stopped gracefully.")

	default:
		fmt.Fprintf(os.Stderr, "Unknown routine action: %s\n", action)
		fmt.Println("Usage: ai-desk routine [list|add|run|pause|resume|delete|daemon]")
	}
}

func cmdVoice(args []string) {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "chat":
		sdb, err := deck.OpenStateDB("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
			return
		}
		defer sdb.Close()

		instances, err := sdb.LoadInstances()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading instances: %v\n", err)
			return
		}

		if len(instances) == 0 {
			fmt.Println("No active agent task instances found in state.db. Create one with `ai-desk task create <title>` first.")
			return
		}

		var target *deck.InstanceRecord
		if len(args) > 1 {
			targetID := args[1]
			for i, inst := range instances {
				if strings.EqualFold(inst.ID, targetID) || strings.EqualFold(inst.Title, targetID) {
					target = &instances[i]
					break
				}
			}
			if target == nil {
				fmt.Fprintf(os.Stderr, "Instance %q not found in state.db.\n", targetID)
				return
			}
		} else {
			// Auto-bind to most recently accessed instance
			target = &instances[0]
			for i := range instances {
				if instances[i].LastAccessed > target.LastAccessed {
					target = &instances[i]
				}
			}
		}

		bridge, err := voice.NewVoiceAgentBridge(sdb, target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error initializing voice bridge: %v\n", err)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\n\033[90mDisconnecting voice bridge...\033[0m")
			cancel()
		}()

		if err := bridge.RunInteractiveLoop(ctx); err != nil {
			fmt.Fprintf(os.Stderr, "Voice bridge terminated: %v\n", err)
		}

	case "attach":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk voice attach <session-id|title>")
			return
		}
		sdb, err := deck.OpenStateDB("")
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
			return
		}
		defer sdb.Close()

		targetID := args[1]
		inst, err := sdb.FindInstance(targetID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Instance %q not found: %v\n", targetID, err)
			return
		}

		sessName, err := voice.StartAgentVoiceSession(inst.ID, inst.Title)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to start agent voice session: %v\n", err)
			return
		}

		fmt.Printf("\033[32m✓ Attached voice bridge for %q to %s\033[0m\n", inst.Title, sessName)
		c := exec.Command("tmux", "attach-session", "-t", sessName)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		_ = c.Run()

	case "status":
		st, err := voice.GetStatus()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error checking voice status: %v\n", err)
			return
		}
		fmt.Println("\n\033[1;36m=== AI Desk Resident Voice Status ===\033[0m")
		if st.VoxInstalled {
			fmt.Printf("  • Vox Binary:     \033[1;32m%s\033[0m\n", st.VoxBinary)
		} else {
			fmt.Println("  • Vox Binary:     \033[1;31mNot found in PATH or ~/.local/bin/vox\033[0m")
		}
		fmt.Printf("  • TTS Model:      \033[1m%s\033[0m\n", st.TTSModel)
		fmt.Printf("  • ASR Model:      \033[1m%s\033[0m\n", st.ASRModel)
		if st.ResidentWarm {
			fmt.Printf("  • Residency:      \033[1;32mWarm (Fast Mode Ready)\033[0m\n")
		} else {
			fmt.Printf("  • Residency:      \033[1;33mCold (Daemon Standby)\033[0m\n")
		}
		daemonLabel := "\033[33mStandby\033[0m"
		if st.DaemonActive {
			daemonLabel = "\033[32mActive (Warm Resident Daemon)\033[0m"
		}
		fmt.Printf("  • Daemon:         %s\n", daemonLabel)
		fmt.Printf("  • Target Latency: < %d ms\n", st.LatencyMS)
		fmt.Println()

	case "start":
		sessName, err := voice.StartFastSession()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error starting fast voice session: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Voice session %q ready!\033[0m\n", sessName)
		fmt.Println("Attaching to tmux...")
		c := exec.Command("tmux", "attach-session", "-t", sessName)
		c.Stdin = os.Stdin
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		if err := c.Run(); err != nil {
			fmt.Fprintf(os.Stderr, "Tmux attach finished: %v\n", err)
		}

	case "speak":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk voice speak <text> [--voice <name>] [--out <output.wav>]")
			return
		}
		text := args[1]
		voiceName := "alba"
		outPath := ""
		for i := 2; i < len(args); i++ {
			if (args[i] == "--voice" || args[i] == "-v") && i+1 < len(args) {
				voiceName = args[i+1]
				i++
			} else if (args[i] == "--out" || args[i] == "-o") && i+1 < len(args) {
				outPath = args[i+1]
				i++
			}
		}
		wav, err := voice.Speak(text, voiceName, outPath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error synthesizing speech: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Synthesized audio:\033[0m %s\n", wav)

	case "hear":
		timeoutSec := 10
		lang := ""
		for i := 1; i < len(args); i++ {
			if (args[i] == "--timeout" || args[i] == "-t") && i+1 < len(args) {
				fmt.Sscanf(args[i+1], "%d", &timeoutSec)
				i++
			} else if (args[i] == "--lang" || args[i] == "-l") && i+1 < len(args) {
				lang = args[i+1]
				i++
			}
		}
		fmt.Printf("Listening for speech (timeout=%ds)... Speak now into microphone:\n", timeoutSec)
		transcript, err := voice.Hear(timeoutSec, lang)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error transcribing audio: %v\n", err)
			return
		}
		fmt.Printf("\n\033[1;32mTranscription:\033[0m %s\n", transcript)

	case "daemon":
		bin := voice.FindVoxBinary()
		if bin == "" {
			fmt.Println("Error: vox binary not found; install vox to ~/.local/bin/vox")
			return
		}
		sub := "status"
		if len(args) > 1 {
			sub = args[1]
		}
		c := exec.Command(bin, "daemon", sub)
		c.Stdout = os.Stdout
		c.Stderr = os.Stderr
		_ = c.Run()

	default:
		fmt.Fprintf(os.Stderr, "Unknown voice action: %s\n", action)
		fmt.Println("Usage: ai-desk voice [status|start|speak|hear|daemon]")
	}
}

func cmdProfile(args []string) {
	cfg, err := rotation.LoadAccountProfiles("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading account profiles: %v\n", err)
		return
	}

	action := "list"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "list":
		fmt.Printf("\n\033[1;36m=== Account Profiles & Routing Policies ===\033[0m\n")
		fmt.Printf("Active Profile: \033[1;32m%s\033[0m\n\n", cfg.ActiveProfile)

		for name, p := range cfg.Profiles {
			activeBadge := ""
			if name == cfg.ActiveProfile {
				activeBadge = " \033[1;32m[ACTIVE]\033[0m"
			}
			failoverBadge := "\033[32menabled\033[0m"
			if !p.AutoFailover {
				failoverBadge = "\033[31mdisabled\033[0m"
			}
			fmt.Printf("  • \033[1m%-16s\033[0m policy=\033[35m%-12s\033[0m auto_failover=%s%s\n", name, p.Policy, failoverBadge, activeBadge)
			if p.Description != "" {
				fmt.Printf("      Description: %s\n", p.Description)
			}
			if len(p.Accounts) > 0 {
				fmt.Printf("      Accounts:    %s\n", strings.Join(p.Accounts, ", "))
			} else {
				fmt.Printf("      Accounts:    (all discovered accounts)\n")
			}
			fmt.Println()
		}

	case "switch", "use":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk profile switch <profile_name>")
			return
		}
		target := args[1]
		if _, ok := cfg.Profiles[target]; !ok {
			fmt.Fprintf(os.Stderr, "Profile %q not found in configuration. Available: default, work, fast\n", target)
			return
		}
		cfg.ActiveProfile = target
		if err := cfg.Save(""); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving account profiles: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Active account profile switched to %q!\033[0m\n", target)

	case "assign", "add":
		if len(args) < 3 {
			fmt.Println("Usage: ai-desk profile assign <profile_name> <account_email>")
			return
		}
		targetProf := args[1]
		accountEmail := args[2]
		if err := cfg.AssignAccount(targetProf, accountEmail); err != nil {
			fmt.Fprintf(os.Stderr, "Error assigning account: %v\n", err)
			return
		}
		if err := cfg.Save(""); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Account %s assigned to profile %q!\033[0m\n", accountEmail, targetProf)

	default:
		fmt.Println("Usage: ai-desk profile [list|switch|assign]")
	}
}

func cmdStack(args []string) {
	if len(args) < 1 {
		fmt.Println("Usage: ai-desk stack <url> [--json]")
		fmt.Println("Example: ai-desk stack https://vercel.com")
		return
	}

	targetURL := args[0]
	jsonOutput := false
	for _, a := range args[1:] {
		if a == "--json" || a == "-j" {
			jsonOutput = true
		}
	}

	if !jsonOutput {
		fmt.Printf("Reverse-engineering tech stack for %s in under 10 seconds...\n", targetURL)
	}

	report, err := assistant.AnalyzeTechStack(targetURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "\033[31mError analyzing tech stack: %v\033[0m\n", err)
		return
	}

	if jsonOutput {
		data, _ := json.MarshalIndent(report, "", "  ")
		fmt.Println(string(data))
		return
	}

	fmt.Print(assistant.FormatReportTable(report))
}

func cmdAssist(args []string) {
	sdb, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error opening state.db: %v\n", err)
		return
	}
	defer sdb.Close()

	virt := harness.NewVirtualizer("", "")
	cfg := assistant.DefaultAssistantConfig()
	ast := assistant.NewVirtualAssistant(sdb, virt, cfg)

	action := "status"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "status", "briefing", "report":
		fmt.Print(ast.GenerateBriefing())

	case "daemon", "monitor", "auto":
		fmt.Println("\n\033[1;36m=== Starting AI-Desk Resident Virtual Assistant Daemon ===\033[0m")
		fmt.Println("Grok-bot mode activated: Monitoring active sessions, tracking turns, and auto-failing over on 429 quota depletion.")
		fmt.Println("Press Ctrl+C to exit.")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigChan
			fmt.Println("\nStopping Virtual Assistant daemon...")
			cancel()
		}()

		ast.StartDaemon(ctx)

	case "failover":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk assist failover <session_id_or_title>")
			return
		}
		sess := args[1]
		fmt.Printf("Triggering failover for session %q...\n", sess)
		updated, err := deck.AutoFailoverSession(sdb, virt, sess, "Manual operator failover")
		if err != nil {
			fmt.Fprintf(os.Stderr, "\033[31mFailover failed: %v\033[0m\n", err)
			return
		}
		fmt.Printf("\033[32m✓ Session %q successfully failed over to account %s!\033[0m\n", updated.Title, updated.Account)

	default:
		fmt.Println("Usage: ai-desk assist [status|daemon|failover <session>]")
	}
}

func runTUI() {
	p := tea.NewProgram(ui.NewModel(), tea.WithAltScreen(), tea.WithMouseCellMotion())
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
	case "account", "accounts":
		cmdAccounts(subArgs)
	case "profile", "profiles":
		cmdProfile(subArgs)
	case "stack", "techstack":
		cmdStack(subArgs)
	case "assist", "auto":
		cmdAssist(subArgs)
	case "run":
		cmdRun(subArgs)
	case "routine", "routines":
		cmdRoutine(subArgs)
	case "voice":
		cmdVoice(subArgs)
	case "skills":
		cmdSkills(subArgs)
	case "rtk":
		cmdRTK(subArgs)
	case "group":
		cmdGroup(subArgs)
	case "session":
		cmdSession(subArgs)
	case "task":
		cmdTaskCreate(subArgs)
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
