---
name: multica-projects-and-resources
description: "Use when creating, inspecting, updating, or debugging Multica projects and project resources. Covers durable project context, github_repo and local_directory resources, how resources affect future agent task context, when to bind repos, and when not to mutate resources."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Projects and Resources

## Quick start

Projects are durable context containers. Resources attached to a project can affect future agent tasks.

```bash
multica project list --output json
multica project get <project-id> --output json
multica project resource list <project-id> --output json
```

Project resources are mutated through project resource commands/endpoints. Issue
comments do not create durable project resources.

## Core model

A project groups work and carries durable resources. A resource is not just display metadata; it is context later injected into task briefs and `.multica/project/resources.json`.

A project's `description` is also durable context: when an issue (or a quick-create task) is bound to a project, the project description is injected into the agent's brief under `## Project Context` and written to `.multica/project/resources.json` as `project_description`. Use it for project-wide rules/context that should apply to every task in the project.

Common resource types:

- `github_repo` — durable GitHub repo context, with `resource_ref.url`, optional checkout `ref`, and optional prompt-only `default_branch_hint`;
- `local_directory` — daemon-local path context, with `resource_ref.local_path`, `daemon_id`, and optional label. Two extra optional fields opt into MUL-3483's parallel mode:
  - `mode` (`"in_place"` default | `"worktree_pool"`).
  - `pool_root` — absolute directory where per-task `git worktree` checkouts live. Defaults to `<parent-of-local_path>/.multica-worktrees/<local_path-basename>` (next to the repo, NOT inside it — the daemon needs write permission on that parent). Users mixing multiple projects should point each ref at a distinct `pool_root` under an explicit daemon-owned tree if they want centralised cleanup.
  - `max_parallel` — cap on concurrent worktrees per base repo (defaults to 4). The N+1th task parks with `wait_reason = "worktree_pool saturated ..."` until a slot frees.
  Worktrees stay on branch `multica/<task-uuid>`. Dirty worktrees are preserved on disk after task exit; users clean them via `git worktree remove` or manual `rm -rf` + `git worktree prune`. Repos with initialised submodules currently refuse `worktree_pool` mode.

## CLI

```bash
multica project list --output json
multica project get <project-id> --output json
multica project create --title "<title>" --repo <github-url> --output json
multica project update <project-id> --title "<title>" --output json
multica project status <project-id> in_progress --output json
multica project resource list <project-id> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
multica project resource update <project-id> <resource-id> --url <new-github-url> --output json
multica project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
multica project resource remove <project-id> <resource-id> --output json
```

For `github_repo`, non-JSON `--ref` sets `resource_ref.ref`, the default checkout branch/tag/SHA for future tasks in that project. JSON `--ref '<json>'` remains the escape hatch for full payloads or resource types not covered by shortcuts.

## When to add a resource

Add/update a project resource when the user asks for durable project context: "把这个 GitHub repo 绑到项目上", "以后都用这个 repo", "agent 总是拿不到这个项目的仓库", or "这个项目要在我的本地目录里跑".

Project resources are durable and affect future tasks. `multica repo checkout`
is task-local checkout state.

## Debugging wrong context

1. `multica project get <project-id> --output json`.
2. `multica project resource list <project-id> --output json`.
3. Check `github_repo.resource_ref.url`, optional `ref`, `default_branch_hint`, and `local_directory.resource_ref.daemon_id`.
4. Updating resources is a durable mutation. After an update, listing the
   resource is the verification path.
5. If resources match the expected task context, inspect runtime/repo checkout
   path next.

## Side effects

Project create/update/delete/status and project resource add/update/remove mutate durable workspace state and affect future tasks. Ask before changing `local_directory` unless the user explicitly requested that exact local path.

More source-backed details: `references/projects-and-resources-source-map.md`.
