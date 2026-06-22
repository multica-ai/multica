---
name: multica-repo-workspace
description: Use when adding GitHub repositories to the Multica workspace-level registry (not project-scoped). Key command is `multica repo add --url <github-url>` for workspace-wide registration. Distinct from `multica project resource add`, which only attaches a repo to a specific project.
---

# Multica Repo Workspace Skill

**Use when registering a GitHub repository with the Multica workspace so all agents can access it.**

## Key Fact

Repositories are registered **workspace-wide** using:

```bash
multica repo add --url <github-url>
```

This makes the repo available to all agents and autopilots in the workspace. It does **not** attach the repo to any specific project.

## Adding a Repo to the Workspace

```bash
multica repo add --url https://github.com/org/repo-name
```

To verify it was registered:

```bash
multica repo list
```

## Common Confusion: Project Resource vs Workspace Registry

| Command | Scope | Effect |
|---|---|---|
| `multica repo add --url <url>` | **Workspace-wide** | Registers repo for all agents |
| `multica project resource add` | **Project-scoped** | Attaches repo to one project only |

Always use `multica repo add` when the goal is workspace-wide availability. Use `multica project resource add` only when you want to scope a repo to a specific project's context.

## Verifying Registration

```bash
# List all workspace-registered repos
multica repo list

# Check out a registered repo in your working directory
multica repo checkout https://github.com/org/repo-name

# Check out a specific branch, tag, or commit
multica repo checkout https://github.com/org/repo-name --ref feature-branch
```

## Tips

- `multica repo checkout` creates a git worktree with a dedicated branch for your agent session.
- Run `multica repo --help` to see all repo subcommands.
- You do not need to register a repo before checking it out; `multica repo checkout` works for any accessible GitHub URL.
