# AI Desk implementation milestones

## Baseline

- Baseline: `87a8477` (v0.3.5), initially clean.
- agent-deck: `01c011b5189ecb0878e8637af076021935b74dab`.
- agy-swap: `0257795e3342f933433840068eb65f3f9a8d2489`.
- Both upstream directories are independent clean checkouts at the pinned revisions. They were preserved.
- Go toolchain available at `/tmp/go/bin/go`; builds do not need to execute the Makefile's RTK installer.
- Baseline failures: two inspector tests assumed personal account data. Fixtures now make these deterministic.

## Stabilization candidate

Session metadata stores an account profile independently from the current credential identity. Existing sessions snapshot the active routing profile on database open. New sessions have their own configuration and skill directories, keyed by session ID. Legacy directories remain usable until a session transitions.

`none` means no ai-desk-selected skills, including across account and harness changes. Runtime built-ins and repository instructions are separate. The skills overlay (`s`) has profiles and a searchable catalog (`Tab`), a preview (`Ctrl+V`), selection (`Enter`), and explicit restart (`Ctrl+R`). Selections are staged until restart. Restart/switch uses a context handoff and a new process; it does not promise lossless native conversation resumption.

Account controls (`a`) allow profile cycling (`p`), toggling that profile's automatic failover (`f`), and choosing its policy (`b`). These last two edit a shared profile definition. `Ctrl+G` in task creation selects the account profile. Routing never broadens a restricted list to outside accounts. Cooldowns persist across monitor/shim processes. Process replacement is serialized and the old session is retained if replacement creation fails.

### CLI

```sh
ai-desk task create example --tool codex --account-profile work --skills none
ai-desk profile session example work
ai-desk skills set example none
ai-desk skills add example verification
ai-desk profile import codex work /path/to/auth.json
ai-desk profile import claude work /path/to/.credentials.json
ai-desk run --account-profile work --skills none codex
```

Codex and Claude imported credentials live in `~/.ai-desk/accounts/<tool>/<id>/`; `AI_DESK_ACCOUNTS_DIR` overrides this location. Use the ID when assigning it to an account profile. Antigravity continues to use agy-swap. The `default` identity means native login and cannot safely auto-failover until a concrete identity is selected. Imported credential availability is not measured provider quota. Keychain-only logins need an adapter before they can be imported as files.

The shim respects the routing profile and uses provider-compatible credentials. Its failover retries the original invocation; it does not append a universal resume flag. Do not use automatic retry for invocations that cannot tolerate replay. The managed-session path instead provides a context handoff.

### Verification and release gate

- Full Go unit suite and both command builds passed.
- Targeted race tests passed for harness, routing, session operations, and shim.
- Real tmux integration passed with fixture CLIs on a temporary socket: creation, replacement, empty skills, and neighboring-session survival.
- `AI_DESK_INTEGRATION=1 go test ./pkg/deck -run TestRealTmuxSessionIsolation -v` repeats that integration check.
- Live speech synthesis is opt-in: `AI_DESK_LIVE_VOICE=1 go test ./pkg/voice -run TestVoice_LocalSynthesisSmoke -v`.
- Installed: Codex 0.153.1, Claude Code 2.1.236, Antigravity 1.4.6.
- Live multi-account authentication/continuation and live speech remain unverified. Two Antigravity accounts were found; alternate Codex/Claude credential locations were requested. Do not tag a release as accepted until these live checks pass.

## Migration and rollback

Back up state.db using SQLite's backup command before running the new version against live data. Metadata additions preserve unrelated tool_data fields and upstream table shapes. Keep the baseline binary available. Rolling back the binary does not require deleting new directories or memory. Avoid switching an active session back to an old account-shared skill directory; finish or explicitly restart it first.

The next branch builds persistent agents and the ADE-inspired TUI. It remains a preview until stabilization's live acceptance gate is met.

### Test isolation correction

The inherited UI tests could open the live session database. An early validation run exercised the profile snapshot migration there; 108 records were subsequently observed with profile metadata. No prior field-level snapshot exists, so exact historical attribution is unavailable. No rollback was attempted that might overwrite newer state. UI tests now use a temporary home, absent database path and tmux shim.

Configuration lookup also resolves the operator home when ai-desk itself runs inside a managed harness, preventing a scoped skill directory from hiding the canonical catalog. `AI_DESK_USER_HOME` can explicitly select that home.
