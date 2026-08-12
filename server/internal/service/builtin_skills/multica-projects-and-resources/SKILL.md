---
name: multica-projects-and-resources
description: "Use when creating, inspecting, updating, or debugging Multica projects and their resources."
user-invocable: false
allowed-tools: Bash(multica *)
---

# Multica Projects & Resources

Projects are durable context containers; attached resources affect future
agent tasks; mutated only via resource commands — comments do NOT create
durable resources. Project resources are durable and affect future tasks;
state in `.multica/project/resources.json` in the workdir.

Types: `github_repo` (`github_repo.resource_ref.url`, `ref`,
`default_branch_hint`); `local_directory` (`resource_ref.local_path`, `daemon_id`, optional
`ref`).

```bash
multica project list --output json
multica project get <project-id> --output json
multica project create --title "<title>" --repo <github-url> --output json
multica project create --title "<title>" --start-date 2026-03-01 --due-date 2026-03-31 --output json
multica project update <project-id> --title "<title>" --output json
multica project update <project-id> --due-date 2026-04-15 --output json
multica project update <project-id> --start-date "" --output json
multica project status <project-id> in_progress --output json
multica project resource list <project-id> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --output json
multica project resource add <project-id> --type github_repo --url <github-url> --ref <branch-or-sha> --output json
multica project resource add <project-id> --type local_directory --local-path <abs-path> --daemon-id <daemon-id> --output json
multica project resource update <project-id> <resource-id> --url <new-github-url> --output json
multica project resource update <project-id> <resource-id> --ref <branch-or-sha> --output json
multica project resource remove <project-id> <resource-id> --output json
```

`github_repo` non-JSON `--ref` sets `resource_ref.ref` (default checkout
branch/tag/SHA). Dates are optional `YYYY-MM-DD`; omitted on update preserved;
`--start-date ""` clears.

No `MUL-123` id for projects — reference via mention-link with the UUID from
`multica project list --output json`:

```
[Roadmap](mention://project/<project-id>)
```

Pure link, no side effects: `util.MentionRe` excludes `project` — nothing
enqueued/notified (same as `issue`). Prefer it over pasting the URL — mobile
hands pasted URLs to the system browser.

Add/update a resource when the user asks for durable project context (e.g.
"bind this GitHub repo to the project"). `multica repo checkout` is task-local
state — not a project resource.

Debug wrong context: 1) `multica project get <project-id> --output json`; 2)
`multica project resource list <project-id> --output json`; 3) check
`resource_ref.url`, `ref`, `default_branch_hint`, `local_path`, `daemon_id`;
4) create/update/delete/status/resource commands mutate durable workspace
state — side effects.

Details: `references/projects-and-resources-source-map.md`.
