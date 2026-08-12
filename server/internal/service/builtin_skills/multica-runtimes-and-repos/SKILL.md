---
name: multica-runtimes-and-repos
description: "Use when a Multica runtime or daemon misbehaves: agent not running, task not claimed, runtime offline, or repo checkout failed."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Runtimes and Repos

## Quick start

Runtime and repo commands affect active agent execution. For "agent did not
run" or "repo checkout failed", read the chain before changing anything:

```bash
multica agent get <agent-id> --output json
multica runtime list --output json
multica repo checkout <repo-url>
```

Commands affect active execution — no speculative restarts/updates.

## Core model

Runtime = execution target behind an agent; daemon owns processes and claims
tasks. Chain: `agent_task_queue` row → task (agent+runtime) → server wakes daemon
websocket → daemon polls/claims the task → server returns context/repos/resources/
hints → daemon prepares workdir, launches provider CLI. `multica repo checkout`
talks to the local daemon, not directly to GitHub.

## CLI

```bash
multica runtime list --output json
multica runtime usage <runtime-id> --output json
multica runtime activity <runtime-id> --output json
multica runtime update <runtime-id> --target-version <version> --output json
multica runtime delete <runtime-id>
multica repo checkout <url>
multica repo checkout <url> --ref <branch-or-sha>
```

`runtime update`/`delete` are writes; update is limited to its owner or a workspace owner/admin; delete refuses while agents are bound unless `--cascade` (unbinds, cancels tasks); unbound agents can't run (`agent_runtime_required`) until `multica agent update <id> --runtime-id <runtime-id>`.
`repo checkout` runs inside a daemon task (`MULTICA_DAEMON_PORT`), creates a
dedicated branch in the task workdir, uses `resource_ref.ref` when set
(else the default ref).

## Debugging "agent did not run"

1. Was a task supposed to be created? Inspect issue/comment/autopilot context.
2. Assignee: agent or squad (squad routes to leader)?
3. Agent archived or bound to a runtime the actor cannot use?
4. Runtime online? `multica runtime list --output json`.
5. Heartbeat fresh? `last_seen_at` is the clue.
6. Task claimed, or stuck pending/running/waiting for local directory?
7. Checkout failed: classify after checking repo context presence.

## Repos

Brief lists repos — authority. Workspace repos ≠ project resources:
`github_repo` = durable context (optional `ref`); `local_directory` =
daemon-owned path. Linux and Windows Codex use task-local Git metadata
(stage/commit without making shared `.repos` writable).

No project resource just because checkout failed — check repo context
presence first.

## Task CLI boundary

The daemon injects a task-scoped `mat_` credential and a private task-local
Multica config root. Inside that context: `issue list`, `issue get`, `issue runs` use the injected task identity, never the daemon Owner's profile; `config show`, `config set` touch only task-local state (missing root fails closed); `auth status` omits tokens; `daemon status`/`daemon disk-usage` report on the task runtime, refuse
`--profile` (`disk-usage` also `--all-profiles`, `--workspaces-root`).
Human/local commands unavailable: `login`, `logout`, `setup`,
`workspace switch`, profile mutation, `daemon start`/`stop`/`restart`,
`daemon logs`, `daemon probe-runtimes` (`daemon stop` kills this daemon
and siblings).

`MULTICA_DAEMON_PORT` alone is defense-in-depth, not task identity —
genuine tasks carry `MULTICA_AGENT_ID` / `MULTICA_TASK_ID`,
`MULTICA_TASK_CONFIG_ROOT`, or a workdir marker; operators should not
export it. Real `HOME`/XDG are preserved for provider tools (`gh`, `aws`,
`kubectl`, npm) — resolution hardening, not confidentiality.

Details: `references/runtimes-and-repos-source-map.md`.
