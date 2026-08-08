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
multica runtime migrate-agents <target-runtime-id> --agent <agent-id> [--agent <agent-id>...] [--from <source-runtime-id>] [--dry-run]
multica repo checkout <url>
multica repo checkout <url> --ref <branch-or-sha>
```

`runtime update` and `runtime delete` are writes. Starting a runtime update is limited to its owner or a workspace owner/admin; the original initiator may keep polling that specific in-flight request if their admin role changes. `runtime delete` removes a runtime registration; if active agents are still bound, it refuses unless the user explicitly passes `--cascade`, which unbinds those agents and cancels their queued/running tasks before deleting the runtime. Unbinding keeps the agents and everything they own — instructions, skills, chats, labels, channel installations, autopilots and task history — and only clears `agent.runtime_id`; an unbound agent cannot run until it is bound to a runtime again (`multica agent update <id> --runtime-id <runtime-id>`), and every trigger path refuses it with `agent_runtime_required`. `repo checkout` creates a dedicated branch in the task working directory. Most runtimes use a linked worktree; Linux and Windows Codex use task-local Git metadata so a task can stage and commit without making the shared `.repos` cache writable.

`runtime migrate-agents` moves agents ONTO the runtime named in the command (the path runtime is the target) and is the recovery path when a machine goes down: rebinding agents one by one leaves their already-queued work behind. Because the daemon lists claim candidates by `agent_task_queue.runtime_id`, a task queued before a plain rebind stays visible only to the runtime the agent left — permanently stuck when that runtime is the dead one. This command moves `queued` and `deferred` tasks along with the agents in the same transaction, then wakes the target runtime so it claims the inherited work instead of waiting out its "no work here" cache.

Tasks already claimed (`dispatched` / `running` / `waiting_local_directory`) are NOT moved: a daemon owns them and is executing them, so they finish where they are and are reported as `tasks_staying_active`. Cancel them explicitly first if you need the runtime fully drained.

`model`, `thinking_level` and `service_tier` are runtime-native, so migration clears them and reports each cleared value; the new runtime then resolves its own defaults. The API request optionally accepts `model` (plus `thinking_level` / `service_tier`, which are model-native and rejected without `model`) as a uniform replacement applied to every migrated agent instead of clearing to the default; values are validated against the target provider's enums, and combining them with `clear_model_settings: false` is a 400. Agents already on the target runtime are NOT skipped: the request is a declarative "set runtime + model settings" write, so they are updated in place (their settings become exactly what the request says, including the runtime default when `model` is empty), which makes the endpoint double as a bulk model change for the current runtime. Their tasks are untouched. Agents you cannot manage come back in `skipped` rather than failing the request — everything else is all-or-nothing.

Three facts about the `model` value before you pass one. It is NOT validated against the runtime's model catalog — custom model ids are allowed by design, so an invented or misspelled id is accepted and stored, and only fails later when a task actually executes. Therefore only pass a model id you have confirmed exists on the target runtime, for example one that another agent bound to that runtime already uses (`multica agent get <agent-id> --output json` shows it); never guess a model id. When you cannot confirm one, omit `model` entirely — an empty model always resolves to the runtime's own default and is always safe. The CLI has no `--model` flag on `runtime migrate-agents` yet: to change models from the CLI, migrate first, then set each agent with `multica agent update <agent-id> --model <id>` (same unvalidated-string caveat applies). `--dry-run` performs every check and count and writes nothing, which is what the confirmation dialog in the UI reads. `--from <source-runtime-id>` makes the server re-derive that runtime's agent set inside the transaction and refuse with `runtime_migration_plan_changed` (409) if it drifted since you listed it; use it whenever you built the agent list from an earlier `runtime list` / agent query. Both the comparison and the 409's `active_agents` list are restricted to agents YOU can see, and that list carries ids and names only — never the agent resource — so a runtime you share with others never discloses their private agents or any secret through this path. Same server endpoint as the UI's Agent List row menu, batch toolbar, agent detail runtime field and the Runtime page's migrate action — one agent is simply a one-element request.

`repo checkout` requires `MULTICA_DAEMON_PORT`; it is intended to run inside a daemon task. If absent, you are not in the normal agent checkout path. When a project `github_repo` resource has `resource_ref.ref`, `repo checkout <url>` uses that ref by default for the current task; an explicit `repo checkout <url> --ref <branch-or-sha>` overrides it.

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
