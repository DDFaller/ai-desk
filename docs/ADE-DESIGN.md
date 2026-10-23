# Persistent-agent TUI design and source map

Reference revision: [damon-ade 8dd78d3815e756381e15b4bf307bd99d0258f6f0](https://github.com/per-simmons/damon-ade/tree/8dd78d3815e756381e15b4bf307bd99d0258f6f0).

## Findings

ADE is an Electron application organized around teams, persistent agents and terminal sessions. Its sidebar calls its top-level project groups Teams. Its model bar opens another CLI session in the current agent workspace. Its file panel groups memory, skills and worktree files. These behaviors translate to terminal navigation without adopting Electron or its application services.

Sources inspected at the pinned revision:

- `apps/desktop/src/renderer/screens/main/components/WorkspaceSidebar/WorkspaceSidebar.tsx`: team navigation.
- `apps/desktop/src/renderer/screens/main/components/WorkspaceView/ContentView/ModelBar/ModelBar.tsx`: runtime session actions.
- `apps/desktop/src/renderer/screens/main/components/WorkspaceView/RightSidebar/AgentFilesView/AgentFilesView.tsx`: file grouping and preview behavior.
- `docs/memory.md`: file-backed identities, bounded notes and generated runtime bridges.
- `LICENSE.md`: Elastic License 2.0. No source, assets, dependencies, or license text were imported into ai-desk.

## Combined architecture

Agent-deck remains the upstream session/group database and tmux foundation. AI Desk owns account selection, session-scoped skills, voice, routines, handoffs and the Bubble Tea interface. ADE supplies organizational inspiration, independently implemented in Go.

`ai_desk_agents` is an additive table holding stable identity, role, team, source repository, worktree, memory home and default runtime/profiles. Sessions keep an `agent_id` in existing JSON tool data; standalone sessions are retained. Tree nodes for agents are virtual, so presentation does not rewrite persisted group paths.

New agents require an existing local Git repository with a commit. Creation makes a separate `ai-desk/<id>` branch and worktree. Adoption associates an existing session without moving its directory. Sessions of a new agent share its worktree; concurrent edits are visible to one another.

Agent memory lives under `~/.ai-desk/agents/<id>/memory`, outside the worktree. Generated AGENTS.md, CLAUDE.md and GEMINI.md bridges live only in session configuration homes. Existing project instruction files are never replaced. Each memory file contributes at most 2,400 characters to the generated bridge. This is a bounded snapshot refreshed on launch; continuous updates become visible next launch. The write-back instructions are conventional guidance, not a guaranteed runtime hook.

Local skills live under the agent's `skills` directory. A derived catalog exposes them with `agent--` names; they require manual attachment. The shared canonical directory and global symlinks remain unchanged.

## TUI and CLI

- `g`: agent manager. `n` creates an agent; `a` archives/restores the selected agent; Enter selects it.
- `n` from the main view: new session, inheriting the selected agent's defaults.
- `Alt+1`, `Alt+2`, `Alt+3`: start Codex, Claude or Antigravity in the selected agent's worktree.
- `Alt+Left/Right`: move between the agent's visible session tabs.
- `m`: browse memory, local skills and tracked worktree files; Enter previews, arrows scroll.
- `s`: browse and manually select skills. Browsing adds nothing to agent context.
- `a`: account/profile controls for the selected session.
- Below 110 terminal columns, Tab switches the single visible pane. Wider terminals show the three panes.

```sh
ai-desk agent create --name Builder --repo /path/to/repo --team Product --tool codex --skills none
ai-desk agent start Builder
ai-desk agent configure Builder --tool claude --account-profile work --skills none
ai-desk agent adopt Builder existing-session
ai-desk agent archive Builder
ai-desk agent restore Builder
ai-desk agent show Builder
```

Default changes affect future sessions only. Archive keeps worktrees and memory. The separate `remove-worktree <agent> --confirm` action requires an archived agent, no live sessions and a clean worktree; it never forces removal.

## Delivery status

The branch is a preview, not a release tag. Unit, race and real tmux fixture checks cover the implementation; live provider authentication, runtime discovery of generated memory bridges, account continuation, voice, and interactive visual review remain release acceptance checks. No hosted deployment, main-branch merge, or public artifact sharing is part of this delivery.

Local review artifact: `.lavish/ai-desk-evolution-plan.html`. Its design source is ai-desk's `pkg/ui/styles.go`; the HTML is intentionally ignored by Git. This document is the durable specification.

## Test-isolation incident

The original UI suite used the real home and could reach the existing session database. An early run exercised the new metadata snapshot migration; 108 existing records were subsequently observed with an account-profile field. There was no pre-run database snapshot, so historical field-level attribution cannot be reconstructed exactly. No rollback was attempted that could overwrite newer state. UI tests now set a temporary HOME, database path and tmux shim. Subsequent agent/worktree tests use temporary repositories and fixture CLIs.
