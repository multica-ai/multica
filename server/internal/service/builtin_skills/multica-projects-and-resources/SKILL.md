---
name: multica-projects-and-resources
description: "Use when creating, inspecting, updating, or debugging Multica projects and their resources (github_repo, local_directory)."
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
- `local_directory` — daemon-local path context, with `resource_ref.local_path`, `daemon_id`, optional label, and
  optional `execution_mode` (`in_place`, the default, or `worktree`).

## CLI

```bash
multica project list --output json
multica project get <project-id> --output json
multica project create --title "<title>" --repo <github-url> --output json
multica project create --title "<title>" --start-date 2026-03-01 --due-date 2026-03-31 --output json
multica project update <project-id> --title "<title>" --output json
multica project update <project-id> --due-date 2026-04-15 --output json
multica project update <project-id> --start-date "" --output json   # clear the start date
multica project status <project-id> in_progress --output json
multica project resource list <project-id> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --execution-mode worktree --output json
multica project resource update <project-id> <resource-id> --execution-mode in_place --output json
multica project resource update <project-id> <resource-id> --url <new-github-url> --output json
multica project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
multica project resource remove <project-id> <resource-id> --output json
```

`--execution-mode` decides how tasks share a `local_directory`. `in_place` (default) runs the agent in the user's
directory, one task at a time; a second task waits in `waiting_local_directory`. `worktree` gives each task its own
git worktree of that repo, so tasks run concurrently and each delivers its work as an `agent/<agent>/<task>` branch
in the user's repo instead of editing the working copy. `worktree` requires the path to be a git repository with at
least one commit; tasks fail with an explicit error otherwise. The daemon version is gated twice — at save time and again
against the daemon that claims each task — so a downgraded machine gets its tasks cancelled rather than run in place. Saving `worktree` is also refused (HTTP 422, code
`daemon_version_unsupported`) while the daemon on that machine is older than the release that ships the mode — the
fix is upgrading that daemon, then retrying. Pass an empty value to clear it back to the default.

For `github_repo`, non-JSON `--ref` sets `resource_ref.ref`, the default checkout branch/tag/SHA for future tasks in that project. JSON `--ref '<json>'` remains the escape hatch for full payloads or resource types not covered by shortcuts.

`--start-date` / `--due-date` are optional calendar days (`YYYY-MM-DD`, like issue dates). On `project update`, pass an empty string (`--start-date ""`) to clear a date; an unset flag leaves it untouched.

## Referring to a project in a comment

A project has no `MUL-123`-style identifier, so writing its title as prose
produces dead text — there is nothing for the reader's client to autolink. Use
the mention-link form instead, with the project UUID from
`multica project list --output json`:

    [Roadmap](mention://project/<project-id>)

Every client makes it navigable, with different presentation: web and desktop
render a chip carrying the project's icon and current title, while mobile
renders an ordinary link that opens the project on tap. Unlike `@agent` /
`@squad`, it is a pure link: `util.MentionRe` does not even include `project`,
so it enqueues nothing and notifies nobody — the same no-side-effect contract
as an `issue` mention.

Prefer this form over pasting the project's URL. Web and desktop do unfurl a
bare in-app project URL into that same chip, but mobile does not — there a
pasted URL is handed to the system browser and takes the reader out of the app.

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
