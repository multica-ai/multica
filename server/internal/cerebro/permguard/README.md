# permguard — permission-gate regression guard (JEH-1171)

This package is an automated tripwire for permission-gate coverage across
the three external surfaces of Multica/Cerebro: **HTTP routes**, **MCP
tools**, and **CLI commands**. The gap-audit on [JEH-1161] discovered that
0 of 63 operations were fully covered on every surface — only because we
went looking. The point of this package is to make sure no new operation
sneaks into the codebase without a conscious decision about who is allowed
to call it.

## How it works

`inventory.json` lists every known mutating operation on every surface.
Three tests sit on top of this file:

| Surface | Source of truth         | Test                                                                          |
| ------- | ----------------------- | ----------------------------------------------------------------------------- |
| http    | `chi.Walk` on the live router | `TestPermissionGuard_HTTPRoutesInventoried` (`server/cmd/server/`)        |
| mcp     | `srv.Tools()` after `registerTools` | `TestPermissionGuard_MCPToolsInventoried` (`server/cmd/multica/`)  |
| cli     | cobra command tree     | `TestPermissionGuard_CLICommandsInventoried` (`server/cmd/multica/`)         |

Each test extracts the operations the running binary actually exposes,
diffs the set against `inventory.json`, and **fails** when:

1. **Missing** — an operation exists in code but not in the inventory
   (regression). Add a new entry to `inventory.json` for the operation.
2. **Stale** — an inventory entry refers to an operation that no longer
   exists. Remove the entry.
3. **Unclassified** — an entry has neither `gate` nor `exempt` set. Pick
   one (see below).
4. **Ambiguous** — an entry has both `gate` and `exempt` set. Pick one.

The test output prints the exact diff so you know what to change.

## Field semantics

```json
{
  "id":     "POST /api/agents/",
  "gate":   "cerebro_capability:create_agent"
}
```

| Field    | When to use                                                                                                                   |
| -------- | ----------------------------------------------------------------------------------------------------------------------------- |
| `id`     | Required. Unique within its surface. For HTTP: `"METHOD /path"`. For MCP: tool name. For CLI: space-separated command path.   |
| `gate`   | Name of the permission check that protects this operation. Non-empty means *gated*.                                           |
| `exempt` | Free-text reason for not having a gate. Non-empty means *explicitly waived*. The reason MUST name a concrete cause.           |

Exactly one of `gate` and `exempt` must be set.

## The gate vocabulary

The vocabulary below is **descriptive**, not enforced by the test — the
test only checks that the field is non-empty. Pick the most specific gate
that applies, and prefer a string that names the check a human can grep
for.

### HTTP

| Gate                              | Where it fires                                                                                              |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------- |
| `daemon_auth`                     | `middleware.DaemonAuth` on `/api/daemon/*`                                                                  |
| `workspace_owner`                 | `middleware.RequireWorkspaceRoleFromURL(..., "owner")`                                                      |
| `workspace_admin`                 | `middleware.RequireWorkspaceRoleFromURL(..., "admin")` or in-handler `isWorkspaceAdmin` check               |
| `workspace_member`                | `middleware.RequireWorkspaceMember` or in-handler `getWorkspaceMember`                                      |
| `project_access`                  | In-handler `canAccessProject` / `canAccessProjectByID`                                                       |
| `issue_scope`                     | `middleware.AllowTaskScopeForIssue` or in-handler `loadIssueForUser`                                        |
| `skill_ownership`                 | In-handler skill-ownership check (`canEditSkill` and friends)                                                |
| `self_only`                       | Endpoint operates on the caller's own data (`/api/me/*`, inbox, pins, push subscriptions)                   |
| `cerebro_capability:<name>`       | `cerebroRequireCapability(w, r, ws, "<name>")` — e.g. `create_agent`, `create_runtime`                      |
| `cerebro_agent_access`            | `cerebroRequireAgentAccess` (group allowlist with owner+capability exemption)                                |
| `cerebro_runtime_access`          | `cerebroRequireRuntimeAccess`                                                                                |

### MCP and CLI

Tools and commands are thin proxies to HTTP. Use the form
`via:<HTTP route>` so a reader can follow the chain to the actual gate.
Read-only entries (those that hit a `GET` endpoint) use the standard
`exempt` form with reason `"read-only — wraps GET …"`.

Local-only commands (config, daemon control, repo checkout) use
`exempt: "local-only — …"` with a one-line description of the side
effect on disk.

## Exemption — when no gate is needed

`exempt` is the explicit "I considered this and decided no gate fires
inside our process". A reason is mandatory because future readers must
be able to challenge the decision without re-deriving it from source.

Common exemption reasons:

- **`pre-auth — …`** — login flow. The endpoint establishes identity, so
  there is no actor to gate on yet. (`/auth/send-code`, `/auth/google`,
  `multica login`.)
- **`bootstrap-token — …`** — short-lived setup token. The token itself
  is the authorisation, before any user session exists.
  (`POST /api/runtime-setup/exchange`.)
- **`daemon-token — …`** — daemon-issued token. The token's binding to a
  runtime is the authorisation. Used inside the HTTP-route inventory
  only when a route falls outside `/api/daemon/*` but is still
  daemon-only.
- **`local-only — …`** — touches only the developer's machine (CLI
  config file, daemon process, local workdir). No server call.
- **`read-only — …`** — wraps a GET endpoint. Reads inherit the GET's
  own access controls (workspace membership, project access). Used in
  the MCP and CLI inventories.

## Adding a new operation

When you add a new mutating route / MCP tool / CLI command:

1. **Implement the gate** in the handler / tool / command body. Use the
   most specific check that fits.
2. **Add an entry** to `inventory.json` under the right surface with
   `gate` set to the name of the check you implemented.
3. Run `go test ./internal/cerebro/permguard/ ./cmd/server/
   ./cmd/multica/` — all three guard tests should be green.

If you genuinely cannot gate the operation, set `exempt` instead and
write a one-line reason that explains why.

## Removing an operation

Delete the entry from `inventory.json`. The guard test will tell you
that the entry is stale if you forget.

## Renaming an operation

Update the `id` in `inventory.json` to match the new code-side name in
the same commit. The guard fails closed: a rename without a matching
inventory update surfaces as one `Missing` plus one `Stale` entry.

## Why three tests instead of one

Each test lives inside the package that owns the surface. HTTP routes
live in `server/cmd/server/`, MCP tools and CLI commands live in
`server/cmd/multica/`, and both packages are `main` packages that
cannot import each other. The shared inventory file is the contract.

[JEH-1161]: https://app.multica.io/workspace/issues/JEH-1161
