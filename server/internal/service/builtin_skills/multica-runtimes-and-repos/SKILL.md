---
name: multica-runtimes-and-repos
description: "Use when a Multica runtime or daemon misbehaves: agent not running, task not claimed, runtime offline, workdir or session reuse, repository checkout."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Runtimes and Repos

## Quick start

For "agent did not run" or "repo checkout failed", read the chain before changing anything:

```bash
multica agent get <agent-id> --output json
multica runtime list --output json
multica repo checkout <repo-url>
```

Runtime and repo commands affect active agent execution. Do not restart daemons, update runtimes, or check out arbitrary repos just to test.

## Core model

A runtime is the execution target behind an agent. A daemon owns local runtime processes and claims queued tasks from the server.

The chain is:

1. user action creates or updates an `agent_task_queue` row;
2. the task points at an agent and runtime;
3. server wakes the runtime over daemon websocket when possible;
4. daemon polls/claims the task;
5. server returns task context, repos, project resources, prior session/workdir hints, and task token;
6. daemon prepares a workdir and launches the provider CLI;
7. `multica repo checkout` talks to the local daemon, not directly to GitHub.

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

`runtime update` and `runtime delete` are writes. Starting a runtime update is limited to its owner or a workspace owner/admin; the original initiator may keep polling that specific in-flight request if their admin role changes. `runtime delete` removes a runtime registration; if active agents are still bound, it refuses unless the user explicitly passes `--cascade`, which unbinds those agents and cancels their queued/running tasks before deleting the runtime. Unbinding keeps the agents and everything they own — instructions, skills, chats, labels, channel installations, autopilots and task history — and only clears `agent.runtime_id`; an unbound agent cannot run until it is bound to a runtime again (`multica agent update <id> --runtime-id <runtime-id>`), and every trigger path refuses it with `agent_runtime_required`. `repo checkout` creates a dedicated branch in the task working directory. Most runtimes use a linked worktree; Linux and Windows Codex use task-local Git metadata so a task can stage and commit without making the shared `.repos` cache writable.

`repo checkout` requires both `MULTICA_DAEMON_PORT` and the injected task-scoped `MULTICA_TOKEN`; it is intended to run inside the active daemon task and from that task's workdir (or a descendant). The local daemon authenticates the token against its active-task registry, derives workspace/task/agent identity itself, and rejects a caller-supplied workdir outside that task. If either variable is absent, you are not in the normal agent checkout path. When a project `github_repo` resource has `resource_ref.ref`, `repo checkout <url>` uses that ref by default for the current task; an explicit `repo checkout <url> --ref <branch-or-sha>` overrides it.

## Task CLI boundary

The daemon injects a task-scoped `mat_` credential for Multica API commands and a private task-local Multica configuration root. Inside that managed task context:

- The `multica` command remains available in every platform-context mode. Official binaries are exposed from their install directory; a locally renamed daemon gets a task-private `multica` alias without requiring a machine-wide symlink.
- API commands such as `issue list`, `issue get`, and `issue runs` use the injected task identity and never fall back to the daemon Owner's saved Multica profile.
- `config show` and `config set` operate only on task-local Multica state. A missing task config root fails closed.
- `auth status` may verify the task identity but omits all token material from its output.
- `daemon status` and `daemon disk-usage` report on the runtime hosting this task: `status` probes the daemon-injected health port, and `disk-usage` scans the daemon-injected workspaces root. Both refuse `--profile`, `disk-usage` also refuses `--all-profiles` and `--workspaces-root`, and its STATUS column stays blank because filling it would spend the Owner's credential.
- Human/local profile and daemon commands — including `login`, `logout`, `setup`, `workspace switch`, local runtime profile path mutation, `daemon start` / `stop` / `restart`, `daemon logs`, and `daemon probe-runtimes` — are unavailable. `daemon stop` in particular would terminate the daemon running this task and every sibling task on it.

`MULTICA_DAEMON_PORT` alone is a weak, defense-in-depth signal for task-safe API and profile resolution, not task identity for human/local command rejection. Genuine tasks also carry `MULTICA_AGENT_ID` / `MULTICA_TASK_ID`, `MULTICA_TASK_CONFIG_ROOT`, or a daemon-managed workdir marker. When the port is the only signal, `daemon status` uses the selected host profile's derived health port; ordinary API and profile-resolving commands still fail closed. Host operators should not export `MULTICA_DAEMON_PORT`; the daemon derives its host health port from the selected profile and injects the variable into tasks itself.

The daemon still preserves the real `HOME` and XDG variables for provider tools such as `gh`, `aws`, `kubectl`, and npm. This is CLI resolution hardening, not hard filesystem confidentiality: a process under the same OS user can still open an explicitly known Owner path. Dedicated Unix users, containers, VMs, or an equivalent OS boundary are required for that stronger isolation.

Host operators can set `platform_context_mode` in the human profile (or start the daemon with `--platform-context` / `MULTICA_PLATFORM_CONTEXT`). `full` preserves the historical workflow brief; `minimal` keeps only runtime facts and a neutral catalog of task-bound skills, so server-provided skills stay visible without Multica imposing activation rules; `off` removes the brief and server-provided skills. `--no-platform-context` remains an alias for `off`. User-installed and workspace-assigned skills remain available in every mode. This is a host-profile setting: changing task-local config from inside a managed task does not reconfigure the daemon.

## Debugging an agent that did not run

Check in this order:

1. Was a task supposed to be created? Inspect issue/comment/autopilot context.
2. Is the assignee an agent or squad? A squad routes to its leader.
3. Is the agent archived or bound to a runtime the actor cannot use?
4. Is the runtime online? `multica runtime list --output json`.
5. Did the daemon heartbeat recently? Runtime `last_seen_at` is the visible clue.
6. Did the task get claimed or is it stuck pending/running/waiting for local directory?
7. If repo checkout failed, classify it after checking whether repo context was
   present in the task/project context.

## Repos

The runtime brief lists repos available to this task. Treat that list as the authority for agent checkout unless the user explicitly asks to bind a new project resource.

Workspace repos and project resources are not the same thing:

- workspace repo metadata can appear in workspace context;
- `github_repo` project resources are durable project context and can affect future tasks; optional `resource_ref.ref` pins the default checkout ref for tasks in that project;
- `local_directory` resources point at a path owned by a daemon and carry local-machine assumptions.

Do not add a project resource just because `repo checkout` failed. First determine whether the user asked for durable project context or just a task checkout.

More source-backed details: `references/runtimes-and-repos-source-map.md`.
