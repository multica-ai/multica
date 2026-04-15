---
name: multica
description: Manage Multica issues, projects, and agents from the terminal
version: 0.1.0
tools: Bash
---

# Multica

Manage your Multica workspace without leaving the terminal. All read commands use `--output json`.

## Prerequisites

The `multica` CLI must be installed and authenticated. If any command fails with "command not found", tell the user:

```
Install: curl -fsSL https://raw.githubusercontent.com/multica-ai/multica/main/scripts/install.sh | bash
Login:   multica login
```

## Issues

### List issues
```bash
multica issue list --output json [--status STATUS] [--priority PRIORITY] [--assignee NAME]
```
Statuses: backlog, todo, in_progress, in_review, done, blocked, cancelled
Priorities: urgent, high, medium, low, none

### Get issue details
```bash
multica issue get <id-or-identifier> --output json
```

### Create issue
```bash
multica issue create --title '...' [--description '...'] [--status STATUS] [--priority PRIORITY] [--assignee NAME] [--project PROJECT_ID] [--ac 'criterion'] [--scope 'pattern'] [--context-ref 'type:ref']
```
`--ac`, `--scope`, and `--context-ref` can be specified multiple times.

### Update issue
```bash
multica issue update <id> [--title '...'] [--description '...'] [--status STATUS] [--priority PRIORITY]
```

### Change status
```bash
multica issue status <id> <STATUS>
```

### Search
```bash
multica issue search 'query' --output json
```

### Assign
```bash
multica issue assign <id> --to <name>
multica issue assign <id> --unassign
```

### Comments
```bash
multica issue comment list <issue-id> --output json [--limit N] [--offset N]
multica issue comment add <issue-id> --content '...' [--parent <comment-id>]
multica issue comment delete <comment-id>
```
For long or multi-line comments, pipe content via stdin:
```bash
echo 'Long comment text here...' | multica issue comment add <issue-id> --content-stdin
```

### Execution history
```bash
multica issue runs <issue-id> --output json
multica issue run-messages <task-id> --output json [--since <seq>]
```

## Repositories

```bash
multica repo checkout <url>
```
Checks out a repo into the working directory as a git worktree with a dedicated branch.

## Projects

### List / Get
```bash
multica project list --output json [--status STATUS]
multica project get <id> --output json
```
Statuses: backlog, planned, in_progress, completed, cancelled

### Create / Update
```bash
multica project create --name '...' [--description '...'] [--status STATUS]
multica project update <id> [--name '...'] [--status STATUS]
```

### Change status
```bash
multica project status <id> <STATUS>
```

### Delete
```bash
multica project delete <id>
```
**Always confirm with the user before deleting.**

## Workspace & Agents

```bash
multica workspace get --output json
multica workspace list --output json
multica workspace members --output json
multica agent list --output json
multica agent get <id> --output json
```

## Rules

- **Shell safety** — Single-quote user text in arguments. For multi-line or special-character content (comments, descriptions), prefer `--content-stdin` via pipe instead of inline arguments.
- **Confirm before destructive ops** — Always ask user to confirm before `delete`, `--unassign`, or bulk status changes.
- **Show identifiers** (PROJ-42) to users, not UUIDs.
- **After changes**, show the result to confirm.
- **Pagination** — If output contains `has_more: true`, continue fetching with `--offset`.

## Error Handling

- "command not found: multica" → install instructions above
- 401 / "not authenticated" → `multica login` (or `multica login --token <PAT>` in headless environments)
- 404 / "not found" → check the ID/identifier, run `multica issue list` to find it
- "no watched workspaces" → `multica workspace list` then `multica workspace watch <id>`
- Network errors / timeouts → check server URL with `multica config show`

## Output Reference

Issue JSON fields: id, identifier, title, description, status, priority, assignee_type, assignee_id, project_id, acceptance_criteria, scope, context_refs, created_at, updated_at

Project JSON fields: id, name, description, status, progress (total, completed, percent), created_at
