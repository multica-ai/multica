---
name: multica-operator
description: Use when a user wants to connect, sign in, configure, inspect, or operate Multica, or turn a business goal into a workflow across Agent, Squad, Skill, Issue, Project, and Autopilot resources from Codex, Claude Code, Cursor, Kiro, or another agent host.
---

# Multica Operator

Use the official community `multica` CLI. Keep internal routing and setup checks
quiet, inspect before changing state, and report only results verified by the
CLI.

## First connection

Once in the current agent session, read `references/version-check.md` before
`references/setup-and-connection.md`. Complete its non-blocking check before
any CLI capability, connection, authentication, or workspace command; it reads
the local `VERSION` file without changing the currently loaded Skill.

Before any connection, authentication, or workspace command, read `references/setup-and-connection.md`.
Account creation uses official browser authentication; never ask for passwords,
verification codes, or tokens in chat.

After setup, verify workspace access with the community CLI:

```bash
multica workspace list --output json
```

Treat valid workspace JSON as the evidence that the selected CLI context can
reach Multica. `[]` means the connection is usable but the account has no
accessible workspace. The community CLI does not expose a safe structured
identity/status command, so do not claim an account identity or effective server
unless the user supplied that information explicitly. Do not run text auth
status because older versions may print token material.
Keep the selected `--profile` and explicit `--server-url` on every command when
the user selected either; never inspect one target and operate on another.

After connection, do not preload or list workspace resources. Query issues,
autopilots, projects, labels, agents, skills, or squads only when the user asks.

When a user choice is required, use explicit selection controls. Present two or
more mutually exclusive choices as a numbered list (`1.`, `2.`, `3.`). Accept a
number-only reply by mapping it to the displayed option; ask again only when the
number is absent, invalid, or the answer would materially change the requested
scope.

## Request routing

Keep a concrete action on a known target in the direct-operation flow below.
For an open-ended business goal, business orchestration, resource selection, or
dependent mutations, read `references/orchestration.md` before querying
workspace resources or proposing a solution. Route by the need for
orchestration, not by resource count. This is operational design, not software
design; use the in-chat plan and confirmation flow defined there.

## Operating rule

Use only commands exposed by the installed `multica` CLI. If the CLI has no
command for the requested operation, explain that the operation is not
currently supported by the CLI and direct the user to complete it in Multica
Web. Do not read a saved profile token, call the Multica API directly, or use
`curl` or another generic HTTP client to bypass the CLI.

For each request:

1. Resolve the requested profile and workspace, and preserve any explicit server target.
2. Query relevant Multica resources before proposing new ones.
3. Preview writes with their target workspace and material fields.
4. Execute only the confirmed scope.
5. Verify the resulting resource before claiming success.

A successful command result is already fresh verification evidence. Do not
repeat the same read solely to satisfy the final verification step.

Read `references/source-map.md` before changing CLI or server claims.

Read `references/extensions.md` when another installed skill declares that it
extends Multica Operator or when a request may require private business policy.
