---
name: multica-cli
description: Operate Multica through its CLI from an external coding agent; read issues, make authorized changes, and prepare or reuse a project's execution workspace.
---

# Multica CLI

This guide ships with the CLI that prints it. It grants no permissions and
does not install files into your agent's skills directory. Existing standalone
skills and workspace skills remain independent. `multica skill` manages
workspace skills; `multica skills` reads these bundled guides offline.

## Start with the user's scope

For an external agent, check the installed CLI and selected account before
accessing a workspace:

```bash
multica version
multica auth status
multica config show
```

Do not print or persist credentials. If not authenticated, have the user run
`multica login`. Use the intended profile/workspace explicitly when supplied:

```bash
multica --profile <profile> --workspace-id <workspace-id> issue get <issue-id> --output json
```

Inside an existing Multica-managed run, keep the supplied task identity and
workspace; do not replace it with a personal login. Follow the run's brief and
platform skill for its issue, trigger comment, and workflow contracts.

## Read only what the task needs

- Issue reads, replies, updates, and run side effects:
  `multica skills read multica-cli references/issues.md`
- Before handing off repeatable code work, or diagnosing repeated setup:
  `multica skills read multica-cli references/workspaces.md`

These commands read the matching reference from the installed binary, including
when the standalone skill is absent or stale. To discover all bundled files:

```bash
multica skills list --output json
```

Use `multica <command> --help` for exact flags and additional namespaces. Prefer
`--output json` for supported data reads; keep stderr separate from stdout.
Use returned IDs, never invented UUIDs or names where an ID is required.

Read state before changing it. Execute writes the user has authorized; ask
about missing scope before unrequested mutations. Assignment, status changes,
mentions and reruns can start paid agent work. Loading a guide authorizes none
of them. If a command is unavailable or refused, report the limitation rather
than bypassing it through a private API.
