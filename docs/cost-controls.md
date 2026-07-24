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

Per-provider ceilings (`--max-turns`, `--max-budget-usd`) fire cleanly
on the runtime side and stop the bleed before the daemon's timeout
wall has to.

## What `custom_args` is

See [`server/pkg/agent/agent.go`](../server/pkg/agent/agent.go) —
`ExecOptions.CustomArgs` is a `[]string` appended after the daemon's
hardcoded protocol flags, filtered against a per-backend blocklist of
protocol-critical flags that the daemon controls.

There are in fact **two** operator-controlled layers, applied in order:

1. **Daemon-wide defaults** — `MULTICA_CLAUDE_ARGS` /
   `MULTICA_CODEX_ARGS`, read by the daemon
   ([`server/internal/daemon/config.go`](../server/internal/daemon/config.go)),
   shell-word parsed, and flowed in as `ExecOptions.ExtraArgs`. These
   apply to every agent of that backend. (Also documented in
   [`CLI_AND_DAEMON.md`](../CLI_AND_DAEMON.md).)
2. **Per-agent `custom_args`** — `ExecOptions.CustomArgs`, set on a
   single agent.

Both are filtered against the same blocklist; `ExtraArgs` is appended
**before** `custom_args`, so a per-agent value wins on a repeated flag.

For Claude Code
([`server/pkg/agent/claude.go`](../server/pkg/agent/claude.go), `buildClaudeArgs`)
the effective command is:

```
claude -p \
    --output-format stream-json \
    --input-format stream-json \
    --verbose \
    --strict-mcp-config \
    --permission-mode bypassPermissions \
    --disallowedTools AskUserQuestion \
    [--model <opts.Model>] \
    [--effort <opts.ThinkingLevel>] \
    [--max-turns <opts.MaxTurns>] \
    [--append-system-prompt <opts.SystemPrompt>] \
    [--resume <opts.ResumeSessionID>] \
    <filtered MULTICA_CLAUDE_ARGS (daemon-wide ExtraArgs)> \
    <filtered custom_args (per-agent)> \
    [--mcp-config <path>]
```

The full ordering is: **hardcoded protocol flags → per-task flags
(`--model`, `--effort`, `--max-turns`, …) → daemon-wide env defaults
(`MULTICA_CLAUDE_ARGS` / `ExtraArgs`) → per-agent `custom_args` →
daemon-owned tail args (e.g. `--mcp-config`)**. If any user-supplied
arg matches a protocol-critical flag (see
[Blocked flags](#blocked-flags)), the daemon drops it and logs a
warning; everything else passes through untouched.

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
| `--effort`          | Owned by the per-agent `thinking_level` picker — the daemon injects `--effort <thinking_level>` itself. |

This is the blocklist as of writing, not a fixed allowlist of
everything else: the daemon filters known protocol-critical flags, so
treat any future daemon-owned flag as potentially blocked too. The
flags you'd normally reach for here — `--max-turns`,
`--max-budget-usd`, `--model` (though `opts.Model` from the task is
preferred), and `--append-system-prompt` — do pass through today.

A value set in `custom_args` for a blocked flag (e.g. `--effort`) is
silently dropped with the warn-level log above, so the override has no
effect — set `thinking_level` on the agent instead.

## Verifying the change landed

After updating `custom_args`, trigger the agent (or wait for its next
scheduled task) and check the daemon log:

```bash
# The daemon logs the full exec argv for each task (all backends emit
# the same "agent command" key, so this works for claude, codex, and
# every other runtime):
grep 'agent command' ~/.multica/daemon.log | tail -1
```

The daemon logs through a `tint` text handler
([`server/internal/logger/logger.go`](../server/internal/logger/logger.go)),
not JSON, so the line is human-readable — look at the `args=[…]`
field. (Don't pipe it to `jq`; the log is not JSON.) You should see
your flags in that slice. If they're missing, check the warn-level log
for `custom_args: blocked protocol-critical flag, skipping` — you
probably tried to override a member of the blocklist.

> **Heads up.** `filterCustomArgs` only strips the blocklist; it does
> not validate that a flag exists. `claude` hard-errors on an unknown
> option (`error: unknown option '--xyz'`) and exits immediately, so a
> single typo in `custom_args` makes **every** task for that agent
> fail on launch — not silently dropped, but a hard failure. Sanity-
> check flag spelling before saving.

## Related

- [`CLAUDE.md`](../CLAUDE.md) — repo-level architecture guide.
- [Codex sandbox troubleshooting](codex-sandbox-troubleshooting.md) —
  a related operator-facing doc that explains a Codex-specific
  failure mode.
