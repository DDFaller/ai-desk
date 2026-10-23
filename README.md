# ⚡ AI Desk

> **Unified AI Agent Command Center** — Orchestrate, monitor, and switch multi-harness autonomous coding agents (Google Antigravity, Anthropic Claude Code, OpenAI Codex, OpenCode, and Vox Video Studio) with intelligent quota rotation, isolated sandboxing, and real-time terminal telemetry.

---


## Current development versions

- `fix/session-profiles`: v0.3.6 release candidate with per-session account policies, explicit `none` skills, a searchable manual skill browser, and isolated runtime configuration.
- `feat/persistent-agent-tui`: v0.4.0 preview adding persistent identities, dedicated worktrees, bounded memory, agent session tabs, and files inspection. This remains a TUI.

Build without running dependency installers:

```sh
go build -o bin/ai-desk ./cmd/ai-desk
go build -o bin/desk-shim ./cmd/desk-shim
./bin/ai-desk
```

In the preview, press `g` to manage agents, `n` to create a session, `s` to browse skills, `a` for session account/profile controls, and `m` for agent files. `Alt+1/2/3` starts Codex/Claude/Antigravity for the selected agent. Profile selection is independent per session. Skill selections are staged until an explicit restart; browsing does not inject the catalog into the model.

Live multi-account authentication, continuation and voice checks remain release gates. See [implementation and verification notes](docs/IMPLEMENTATION.md) and [the ADE adaptation](docs/ADE-DESIGN.md), including migration and test-isolation details.

## ✨ Features

- **Multi-Harness Process Orchestration**: Seamlessly manage and run agents across **Antigravity (`agy`)**, **Claude Code (`claude`)**, **Codex CLI (`codex`)**, **OpenCode (`opencode`)**, and **Vox Video Studio (`vox-studio`)**.
- **Interactive 3-Pane Terminal UI**:
  - **Pane 1 (Selection Tree)**: Hierarchical tree view of groups, projects, active tasks, and status badges.
  - **Pane 2 (Task Dashboard)**: Live terminal output preview, project path, active skills, and real-time token telemetry.
  - **Pane 3 (Session Status & Quota)**: Live quota gauges, account routing status, uptime, and system diagnostics.
- **Clean Harness Switching (`[w]`)**:
  - Context Handoff synthesis (captures conversation tail, git branch, diff stat, and directive into an actionable brief).
  - In-place clean session recreation (terminates stale processes, purges lingering session IDs, resets scrollback).
  - Fork / Branch into a new task instance while keeping the original agent running.
- **Account Vault & Autonomous Quota Failover**:
  - Multi-account quota tracking and visual consumption meters.
  - Automatic 429 / quota exhaustion failover routing sessions to the next healthy account.
  - Isolated credential sandboxing (`GEMINI_CLI_HOME`, `AI_DESK_ACCOUNT`, and permission mirroring).
- **Conscious Skills Projection**:
  - Curated skill profiles (`core`, `web`, `video`, `career`, `minimal`, `all`) that project only relevant skills into each agent session, reducing injected skill catalog overhead.
- **Agent Deck Architecture**:
  - Full tmux integration with isolated sockets and crash-safe session attachment.
  - Tab path autocomplete, `Ctrl+R` recent paths cycling, `Ctrl+W` path-aware rubout, and instant `Ctrl+S` creation.
- **Resident Virtual Assistant & Voice Engine**:
  - Bidirectional voice chat and status briefings powered by local TTS/STT.
  - SaaS tech stack reverse engineering (`ai-desk stack <url>`) inspired by dapdev & Grok bot architecture.
- **Scheduled Routines**:
  - Recurring background cron agents with execution tracking, status reporting, and persistent outputs in SQLite.
- **RTK (Rust Token Killer) Acceleration**:
  - Condensed command outputs saving 60–90% context tokens without signal loss.

---

## 🚀 Quick Start

### Installation

```bash
# Clone the repository with submodules
git clone --recurse-submodules https://github.com/DDFaller/ai-desk.git
cd ai-desk

# Build and install to ~/.local/bin
make install
```

### Launching the Interactive TUI

```bash
ai-desk
# or
ai-desk tui
```

---

## ⌨️ TUI Keyboard Shortcuts

| Key | Description |
|---|---|
| `Tab` | Cycle focus between Selection (1), Task (2), and Status (3) panes |
| `↑` / `↓` (`k` / `j`) | Navigate tree items, tasks, and dialog options |
| `←` / `→` | Collapse / Expand group folders in the selection tree |
| `Enter` | Attach directly to the selected agent's tmux session |
| `n` | Open the **Create Task** dialog with path auto-completion |
| `w` | Open the **Switch Harness** modal with context handoff & clean restart |
| `a` | Open the **Switch Account** modal for live quota migration |
| `f` | Trigger an **Autonomous Failover** test |
| `r` | View and manage scheduled **Cron Routines** |
| `v` | Connect bidirectional **Voice Chat** |
| `s` | Open the **Skills Catalog** overlay |
| `i` | View deep session inspection details |
| `?` / `Ctrl+P` | Open the **Command Palette** |
| `R` / `F5` | Refresh state, active quotas, and tree from SQLite |
| `q` / `Esc` | Quit AI Desk |

---

## 🛠️ CLI Reference

```bash
# General
ai-desk status                      # Show accounts, quotas, and active sessions
ai-desk accounts list               # Inspect managed accounts & task allocations

# Task & Session Lifecycle
ai-desk task create <title>         # Create and launch an isolated agent task
ai-desk session list                # List active tmux sessions
ai-desk session attach <name>       # Attach directly to an agent session
ai-desk session inspect <id|title>  # Deep telemetry and token breakdown
ai-desk session switch <id> <tool>  # Cross-harness migration with handoff brief
ai-desk session switch <id> <tool> --fork # Fork to a new task instance

# Skills & RTK
ai-desk skills list                 # List skill profiles & prompt token estimates
ai-desk skills show <profile>       # Show detailed skills inside a profile
ai-desk rtk gain                    # Display terminal token savings dashboard

# Routines & Voice
ai-desk routine list                # View scheduled routines
ai-desk routine add --title "Daily Triage" --cron "0 9 * * *" --command "agy run triage"
ai-desk voice status                # Check local speech model daemon status
ai-desk stack https://x.ai          # Reverse-engineer tech stack in 10s
```

---

## 🧪 Testing

AI Desk features a comprehensive test suite covering virtualizers, account rotation, cross-harness switching, autocomplete, and UI models:

```bash
go test ./...
```

---

## 📄 License

MIT License. See [LICENSE](LICENSE) for details.
