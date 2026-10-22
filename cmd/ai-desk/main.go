package main

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"github.com/falluba/ai-desk/pkg/providers"
	"github.com/falluba/ai-desk/pkg/rotation"
	"github.com/falluba/ai-desk/pkg/rtk"
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
	fmt.Println("                      Options: --skills <profile> (auto, minimal, core, web, video, career, all)")
	fmt.Println("  skills [action]     Manage skill profiles & conscious runtime attachment")
	fmt.Println("                      Actions: list, audit, attach <tool> <acc> <skill>, detach <tool> <acc> <skill>")
	fmt.Println("  rtk [action]        Inspect RTK token savings dashboard or initialize harness hooks")
	fmt.Println("                      Actions: gain (default), init, config")
	fmt.Println("  group [action]      Manage hierarchical groups & subgroups")
	fmt.Println("                      Actions: list, create <path>, delete <path> -r, move <session> <group>")
	fmt.Println("  session [action]    Manage agent sessions (list, attach, inspect <id|title>)")
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
		tmuxArgs := []string{"attach"}
		if target != "" {
			tmuxArgs = append(tmuxArgs, "-t", target)
		}
		cmd := exec.Command("tmux", tmuxArgs...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		_ = cmd.Run()
	case "inspect", "info", "show":
		if len(args) < 2 {
			fmt.Println("Usage: ai-desk session inspect <id|title>")
			return
		}
		cmdSessionInspect(args[1])
	}
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
	case "rtk":
		cmdRTK(subArgs)
	case "group":
		cmdGroup(subArgs)
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
