package main

import (
	"flag"
	"fmt"
	"github.com/falluba/ai-desk/pkg/deck"
	"github.com/falluba/ai-desk/pkg/harness"
	"os"
)

func cmdAgent(args []string) {
	if len(args) == 0 {
		fmt.Println("ai-desk agent list|create|start|adopt|archive|restore|remove-worktree|show")
		return
	}
	db, err := deck.OpenStateDB("")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return
	}
	defer db.Close()
	fail := func(err error) {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	switch args[0] {
	case "list":
		agents, err := db.ListAgents(true)
		if err != nil {
			fail(err)
			return
		}
		for _, a := range agents {
			fmt.Printf("%s  %s/%s  %s  archived=%t\n", a.ID, a.Team, a.Name, a.Runtime, a.Archived)
		}
	case "create":
		f := flag.NewFlagSet("agent create", flag.ContinueOnError)
		var opts deck.CreateAgentOptions
		f.StringVar(&opts.Name, "name", "", "Agent name")
		f.StringVar(&opts.Role, "role", "", "Agent role")
		f.StringVar(&opts.Team, "team", "my-agents", "Team/group")
		f.StringVar(&opts.Repository, "repo", ".", "Local Git repository")
		f.StringVar(&opts.Runtime, "tool", "codex", "Runtime")
		f.StringVar(&opts.AccountProfile, "account-profile", "", "Account profile")
		f.StringVar(&opts.SkillProfile, "skills", "none", "Skill profile")
		if err = f.Parse(args[1:]); err != nil {
			return
		}
		a, err := db.CreateAgent(opts)
		if err != nil {
			fail(err)
			return
		}
		fmt.Printf("Created %s (%s)\nWorktree: %s\nMemory: %s/memory\n", a.Name, a.ID, a.Worktree, a.Home)
	case "configure":
		if len(args) < 2 {
			fmt.Println("ai-desk agent configure <agent> [--tool runtime] [--account-profile name] [--skills profile]")
			return
		}
		f := flag.NewFlagSet("agent configure", flag.ContinueOnError)
		tool := f.String("tool", "", "Runtime")
		profile := f.String("account-profile", "", "Account profile")
		skills := f.String("skills", "", "Skill profile")
		if err := f.Parse(args[2:]); err != nil {
			return
		}
		fail(db.UpdateAgentDefaults(args[1], *tool, *profile, *skills))
	case "start":
		if len(args) < 2 {
			fmt.Println("ai-desk agent start <agent> [runtime]")
			return
		}
		tool := ""
		if len(args) > 2 {
			tool = args[2]
		}
		inst, err := db.CreateTask(harness.NewVirtualizer("", ""), deck.CreateTaskOptions{AgentID: args[1], Tool: tool})
		if err != nil {
			fail(err)
			return
		}
		fmt.Printf("Started session %s\n", inst.ID)
	case "adopt":
		if len(args) < 3 {
			fmt.Println("ai-desk agent adopt <agent> <session>")
			return
		}
		fail(db.AdoptSession(args[1], args[2]))
	case "archive", "restore":
		if len(args) < 2 {
			return
		}
		fail(db.ArchiveAgent(args[1], args[0] == "archive"))
	case "remove-worktree":
		if len(args) < 3 || args[2] != "--confirm" {
			fmt.Println("ai-desk agent remove-worktree <archived-agent> --confirm")
			return
		}
		fail(db.RemoveAgentWorktree(args[1]))
	case "show":
		if len(args) < 2 {
			return
		}
		a, err := db.FindAgent(args[1])
		if err != nil {
			fail(err)
			return
		}
		fmt.Printf("%s\nRole: %s\nTeam: %s\nWorktree: %s\nMemory: %s/memory\nRuntime: %s\nAccount profile: %s\nSkill profile: %s\n", a.Name, a.Role, a.Team, a.Worktree, a.Home, a.Runtime, a.AccountProfile, a.SkillProfile)
	default:
		fmt.Println("Unknown agent action")
	}
}
