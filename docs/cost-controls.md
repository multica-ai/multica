# Cost controls for agent runtimes

This page explains how to bound token spend and runaway turns for
daemon-launched agents (`claude`, `codex`, `gemini`, …) by configuring
the agent's `custom_args`.

The mechanism itself is already built in — the daemon appends each
agent's configured `custom_args` to the hardcoded runtime command
before spawning the process. This page exists so operators don't need
to read the source to discover what they can set.

## Why this matters

When Multica picks up a task, the daemon spawns the runtime CLI
(`claude`, `codex`, …) until it exits or `MULTICA_AGENT_TIMEOUT`
fires. Most providers charge per token, so a poorly-bounded run
consumes budget for the full duration **even when the timeout wall
eventually discards the result**. Two common patterns to guard
against:

| Pattern                           | Observed outcome                                                                  |
| --------------------------------- | --------------------------------------------------------------------------------- |
| Runtime caught in a tool-use loop | Duration grows until `MULTICA_AGENT_TIMEOUT` fires; tokens spent, output dropped. |
| Long-tail exploratory task        | Tool count grows linearly; budget bleed is silent.                                |

Per-provider ceilings (`--max-turns`, `--max-budget-usd`, `--limits`)
fire cleanly on the runtime side and stop the bleed before the
daemon's timeout wall has to.

## What `custom_args` is

See [`server/pkg/agent/agent.go`](../server/pkg/agent/agent.go) —
`ExecOptions.CustomArgs` is a `[]string` appended after the daemon's
hardcoded protocol flags, filtered against a per-backend blocklist of
protocol-critical flags that the daemon controls.

For Claude Code
([`server/pkg/agent/claude.go`](../server/pkg/agent/claude.go)) the
effective command is:

```
claude -p \
    --output-format stream-json \
    --input-format stream-json \
    --verbose \
    --strict-mcp-config \
    --permission-mode bypassPermissions \
    [--model <opts.Model>] \
    [--max-turns <opts.MaxTurns>] \
    [--append-system-prompt <opts.SystemPrompt>] \
    [--resume <opts.ResumeSessionID>] \
    <filtered custom_args> \
    [--mcp-config <path>]
```

`custom_args` lands after the per-task flags (`--model` /
`--max-turns` / `--append-system-prompt` / `--resume`) and before the
daemon-appended `--mcp-config`. If any user-supplied arg matches a
protocol-critical flag (see [Blocked flags](#blocked-flags)), the
daemon drops it and logs a warning; everything else passes through
untouched.

## Setting `custom_args`

### Via the web UI

**Settings → Agents → `<your agent>` → Custom Args**.

The tab accepts a JSON array of strings; each element is one argv
token:

```json
["--max-turns", "60", "--max-budget-usd", "1.00"]
```

### Via the CLI

Same shape, passed as a JSON string:

```bash
multica agent update <agent-id> \
    --custom-args '["--max-turns","60","--max-budget-usd","1.00"]'
```

To verify what the daemon is currently sending, check `daemon.log`
for the `agent command` line that appears when a task starts — it
logs the full `args` slice after filtering.

## Recommended starting values

These are starting points, not authoritative defaults — tune per
agent based on the task shape and observed token cost per tool call.

### Claude Code (`claude -p`)

```json
["--max-turns", "60", "--max-budget-usd", "1.00"]
```

Rationale: `--max-budget-usd` is the primary safeguard — it fires
cleanly when the model's own usage estimate crosses the ceiling, and
the duration cap is just a fallback. `60` turns is generous for most
autopilot-style tasks (observed median on a long-running pipeline:
~46 tools); raise the budget ceiling for agents that routinely do
wide-fanout investigations.

> **Heads up.** `claude -p` with
> `--output-format=stream-json` requires `--verbose` — the CLI exits
> with `When using --print, --output-format=stream-json requires
> --verbose` otherwise. The daemon already emits `--verbose` for
> you; don't try to strip it in `custom_args`.

### Codex (`codex`)

The Codex runtime does not expose a per-run dollar ceiling today; the
equivalent is `MULTICA_AGENT_TIMEOUT` at the daemon level plus any
per-workspace billing quota at your provider.

`codexBlockedArgs` only blocks `--listen` (the daemon's own listener
flag), so the `custom_args` surface for Codex is in fact wider than
for Claude — any other `codex app-server` flag can be passed through
untouched. See
[`server/pkg/agent/codex.go`](../server/pkg/agent/codex.go) for the
current blocklist.

## Blocked flags

Per-backend blocklists live next to each backend (e.g.
`claudeBlockedArgs` in
[`server/pkg/agent/claude.go`](../server/pkg/agent/claude.go)). The
daemon drops these if present in `custom_args` and logs
`custom_args: blocked protocol-critical flag, skipping` at warn
level, because overriding them would break the
daemon↔runtime stream-json protocol.

For the Claude backend the blocked set today is:

| Flag                | Why it's blocked                                                     |
| ------------------- | -------------------------------------------------------------------- |
| `-p`                | Non-interactive mode — daemon owns this.                             |
| `--output-format`   | stream-json is the daemon↔runtime protocol.                          |
| `--input-format`    | stream-json is the daemon↔runtime protocol.                          |
| `--permission-mode` | `bypassPermissions` is required for autonomous task execution.       |
| `--mcp-config`      | Set by the daemon from the agent's `mcp_config` (controlled toolset). |

Everything else is yours to configure. This explicitly includes
`--max-turns`, `--max-budget-usd`, `--model` (though `opts.Model`
from the task is preferred), `--append-system-prompt`, and any
future claude-cli flag.

## Verifying the change landed

After updating `custom_args`, trigger the agent (or wait for its next
scheduled task) and check the daemon log:

```bash
# The daemon logs the full exec argv for each task (all backends emit
# the same "agent command" key, so this works for claude, codex, and
# every other runtime):
grep 'agent command' ~/.multica/daemon.log | tail -1 | jq .args
```

You should see your flags in the `args` array. If they're missing,
check the warn-level log for `custom_args: blocked protocol-critical
flag, skipping` — you probably tried to override a member of the
blocklist.

## Related

- [`CLAUDE.md`](../CLAUDE.md) — repo-level architecture guide.
- [Codex sandbox troubleshooting](codex-sandbox-troubleshooting.md) —
  a related operator-facing doc that explains a Codex-specific
  failure mode.
