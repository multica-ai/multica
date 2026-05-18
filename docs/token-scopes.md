# Token scopes

This document describes how the Multica server authenticates incoming
requests, what each token type represents, and what access each one
grants. It is the canonical reference — when in doubt, prefer
verifying the code against this document, not the other way around.

## Token types

Multica recognises four token formats. All four are passed in the
`Authorization: Bearer <token>` header (or, for JWT only, an HttpOnly
cookie).

| Prefix    | Type           | Lifetime  | Where it's minted                                       | Stored as            |
| --------- | -------------- | --------- | ------------------------------------------------------- | -------------------- |
| _(none)_  | JWT            | 7 days    | `/api/auth/verify` after email-code login               | Signed, not in DB    |
| `mul_`    | PAT            | Forever¹  | `/api/me/tokens` (user-issued personal access token)    | `personal_access_token` (hash) |
| `mdt_`    | Daemon token   | Forever¹  | `/api/runtimes/{id}/setup` when a daemon registers      | `agent_runtime_setup_token` (hash) |
| `mtt_`    | Task token     | 1 hour    | `/api/runtimes/{id}/tasks/claim` when daemon claims a task | `task_token` (hash) |

¹ "Forever" = until the user revokes it; no automatic expiry.

## Auth scopes

`internal/middleware/scope.go` exposes a single `AuthScope` enum:

- `ScopeUser` — the request acts as a workspace member. This is the
  default for every token type.
- `ScopeTask` — *historical*. Was used by JEH-324 to narrow `mtt_`
  tokens to a single task. **No longer assigned anywhere.** Agents
  now act with their owner's full member access (vanilla scope). The
  enum value, the `withTaskScope` helper, and the
  `AllowTaskScopeFor*` / `RequireUserScope` middlewares are kept in
  the codebase as no-ops so we can re-introduce a narrow scope later
  without rewiring routes — but no production traffic ever takes the
  task branch today.

## How each token type authenticates

All four flow through `internal/middleware/auth.Auth`. After the
middleware runs, downstream handlers see:

- `X-User-ID` header — the principal acting on the request
- `X-Agent-ID`, `X-Task-ID` headers — set only for `mtt_` tokens, so
  comments authored during the request are attributed to the agent
  instead of the user
- `AuthScope` in the request context — always `ScopeUser` today
- `WorkspaceID` in the request context — set later by the workspace
  middleware (slug → UUID lookup), not by `Auth`

### JWT (cookie or bearer)

- Validated against `JWT_SECRET` (HMAC-SHA256).
- Cookie path additionally requires a valid CSRF token for state-changing
  methods (`POST`/`PATCH`/`PUT`/`DELETE`).
- Sets `X-User-ID` from the JWT subject claim.
- Scope: `ScopeUser`.

### PAT (`mul_`)

- Hash looked up in `personal_access_token`.
- `last_used_at` is updated best-effort in the background.
- Sets `X-User-ID` from the row's `user_id`.
- Scope: `ScopeUser`.

### Daemon token (`mdt_`)

- Hash looked up in `agent_runtime_setup_token`.
- The token row carries `workspace_id`, which is stored in the request
  context as `DaemonWorkspaceID`. Cross-workspace daemon access is
  rejected by `requireDaemonWorkspaceAccess` (a daemon registered for
  workspace A cannot read workspace B's data even if it knows the UUID).
- `X-User-ID` is *not* set — daemons act as the runtime, not as a user.
- Scope: `ScopeUser`.

### Task token (`mtt_`)

- Hash looked up in `task_token`. Each row binds the token to a
  `(task_id, issue_id, agent_id, workspace_id)` and a 1-hour expiry.
- The token row's `agent_id` is dereferenced to set `X-User-ID` to the
  agent's `owner_id`. The agent therefore acts as its owner: every
  workspace, role, and permission the owner has is granted to the
  agent for the lifetime of the task.
- `X-Agent-ID` and `X-Task-ID` are set so comments and other writes
  show the agent as the author rather than the human owner.
- Agent-authored comments that mention another agent only enqueue the
  mentioned agent when the source task has `original_user_id`. For
  autopilot tasks in both `run_only` and `create_issue` modes, this is
  copied from the autopilot row when `created_by_type = 'member'`;
  agents can inspect it with
  `multica autopilot get <autopilot-id> --output json`
  (`created_by_type`, `created_by_id`).
- The token is revoked by `RevokeTaskTokensForTask` on task
  completion, failure, or cancellation.
- Scope: `ScopeUser` (since the JEH-324 rollback).

## Why `mtt_` still exists if it's not narrowed

Three reasons we kept the per-task token rather than passing the
daemon's `mdt_` (or owner's `mul_` PAT) directly to the agent:

1. **Revocability.** Cancelling, failing, or completing a task drops
   the row from `task_token`. The agent process can no longer act,
   even if it cached the token. A leaked `mul_` PAT or `mdt_` would
   stay valid until manually revoked.
2. **Audit trail.** The `task_token` row records which agent ran which
   task in which workspace. Every authenticated request from the agent
   carries `X-Agent-ID`/`X-Task-ID`, so writes can be attributed to a
   specific run rather than to the daemon as a whole.
3. **TTL.** Even without revocation, the 1-hour expiry caps the blast
   radius of a leak. PATs and daemon tokens have no automatic expiry.

These properties are independent of scope: revocability and audit
hold whether the token is narrow- or wide-scoped.

## Request middleware order

A typical workspace-scoped request enters this stack from outside in:

```
http.Server
  └── chi.Router
        ├── middleware.Auth                  -- sets X-User-ID, AuthScope
        ├── middleware.RequireWorkspaceFromURL("slug")  -- resolves slug → UUID, gates 404
        ├── middleware.RequireWorkspaceMember(...)      -- enforces membership for X-User-ID
        ├── (optional) middleware.RequireWorkspaceRoleFromURL(..., "owner", "admin")
        └── handler.SomeHandler
```

`Auth` does *not* know which workspace is being addressed; the
workspace middlewares run later, take the slug from the URL, and
verify that `X-User-ID` is a member with the required role. This is
why an `mtt_` token works seamlessly for routes the agent's owner
already has access to and is rejected for everything else.

## Adding a new authenticated route

For a new member-facing route:

1. Mount it under the workspace router so `RequireWorkspaceMember`
   runs.
2. If admin-only, also wrap in `RequireWorkspaceRoleFromURL("id",
   "owner", "admin")`.
3. Use `requestUserID(r)` to get the acting user's UUID. For
   comment-style writes that should be attributed to an agent, also
   read `X-Agent-ID`/`X-Task-ID` and call `resolveActor`.

Do **not** add `RequireUserScope` to "lock out task tokens" — task
tokens already act with user scope. Adding it does nothing useful and
adds a layer to reason about.

## History

- **JEH-324 (Part A, 2026-05-01)** — introduced `mtt_` tokens with a
  narrow `ScopeTask`. Agents could only touch their bound issue and
  workspace; everything else returned 403. Rolled back because the
  narrow scope blocked agents from reading sibling issues, parent
  projects, and member listings that the owner could see.
- **2026-05-04 — vanilla scope restored.** `mtt_` tokens now act as
  `ScopeUser`. Middleware wrappers (`AllowTaskScopeFor*`,
  `RequireUserScope`) remain in the tree as no-ops; the
  `withTaskScope` helper is unused.
