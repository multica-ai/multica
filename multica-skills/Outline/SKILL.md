---
name: Outline
description: Per-user-filtered read-only access to the self-hosted Outline at docs.zoop.tools. Inside Multica the requesting user's email is auto-resolved from MULTICA_TASK_ID; outside Multica it is the first positional arg. USE WHEN search outline, find in outline, look up outline, get outline doc, show outline page, list outline collections, list outline documents, what's in outline, outline knowledge base.
---

# Outline (Read-Only, Per-User Filtered)

Read-only client for `https://docs.zoop.tools`. Uses the workspace **admin** token (`OUTLINE_API_KEY`) to call Outline's REST API, but every call is made on behalf of a specific user — the skill post-filters results against that user's collection / group / document memberships before returning anything.

This is the "admin token + post-filter" pattern. It re-implements Outline's permission model client-side, so it is brittle by design — but it gives Multica per-user filtering without reissuing per-user Outline tokens.

## Hard rules

- **Every call must be made on behalf of a specific user.** Pass the email as the first positional arg, OR run inside a Multica agent task — the helper auto-resolves it from `MULTICA_TASK_ID` (see "Multica auto-resolution" below). If neither is available, refuse:
  > Outline lookups require the requesting user's email. Please provide it.
- **Suspended / deactivated users get no access.** The script enforces this; do not try to work around it.
- **Workspace admins bypass the filter** and see everything. This matches Outline's own UI behavior.
- **Read-only.** Never call write endpoints (`documents.create/update/delete/move`, etc.). If the user asks for an edit, respond:
  > This skill is read-only. Make the change in Outline directly at https://docs.zoop.tools.
- **Never print `OUTLINE_API_KEY`.** Keep the literal `$OUTLINE_API_KEY` reference in any command shown to the user.

## Multica auto-resolution

When this skill is attached to a Multica agent and the daemon spawns the agent process, these env vars are set automatically: `MULTICA_TASK_ID`, `MULTICA_AGENT_ID`, `MULTICA_WORKSPACE_ID`, `MULTICA_TOKEN`, `MULTICA_SERVER_URL`. In that context, you may invoke any subcommand with an empty first arg — the helper at `Tools/ResolveTriggerEmail.sh` chains Multica's public API (`/api/agents/{id}/tasks` → `/api/issues/{id}/comments` or `/api/chat/sessions/{id}` → `/api/workspaces/{id}/members`) to resolve the triggering user's email.

This means the skill works without any patches to the Multica server: the email is derived at runtime from data the daemon already exposes. If the resolution fails (e.g. assignment-triggered tasks with no comment/chat), the script exits 2 ("email is required") and you must ask the human for it.

Standalone CLI usage outside Multica is unchanged — pass the email as the first arg.

## Prerequisite

`OUTLINE_API_KEY` must be set in the environment. Verify before any call:

```bash
[ -n "$OUTLINE_API_KEY" ] && echo "ok" || echo "missing"
```

If missing, stop and tell the user:
> `OUTLINE_API_KEY` is not set. Generate an **admin** token at `https://docs.zoop.tools/settings/api-tokens` and run `export OUTLINE_API_KEY="<token>"` (or set it in the agent's Custom Env / daemon container env when running in Multica).

## Helper script

All API calls go through the helper at `Tools/Outline.sh`. It handles:

1. Resolving `<email>` → workspace user (rejects unknown / suspended users).
   - Auto-resolves from `MULTICA_TASK_ID` when the email arg is empty inside a Multica task.
2. Bypassing the filter for `role == "admin"`.
3. Computing the user's visible collection set from `collections.list` + `collections.memberships` + `collections.group_memberships` + `groups.list?userId=…`.
4. Falling back to `documents.memberships` for explicit doc-level grants when fetching a single doc.

Invoke as (relative to the skill directory):

```bash
bash Tools/Outline.sh <subcommand> <email> ...
```

## Workflows

In all workflows below, `<email>` may be the empty string `""` when running inside a Multica agent task — the helper auto-resolves it. Outside Multica, supply a real email.

### 1. Search documents
**Triggers:** "search outline for X", "find X in outline", "look up X in our docs"

```bash
bash Tools/Outline.sh search "<email>" "<query>" 10
```

Returns up to 10 matches the user can see, each with `title`, `url`, `snippet`.

### 2. Get a document by ID or slug
**Triggers:** "show me the X doc", "get outline doc <id>", "open <slug> from outline"

```bash
bash Tools/Outline.sh get "<email>" "<doc_id_or_slug>"
```

Returns `{title, url, text}` (full markdown body) if the user has access, otherwise exits non-zero with an `access denied` error. `id` accepts UUID or URL slug (e.g. `engineering-handbook-AbCdEf`).

### 3. List collections
**Triggers:** "list outline collections", "what collections do we have", "show outline workspaces"

```bash
bash Tools/Outline.sh collections "<email>"
```

Only collections the user can access are returned.

### 4. List documents in a collection
**Triggers:** "list docs in <collection>", "show me everything in <collection>"

Resolve the collection ID via workflow 3 first if the user gave a name, then:

```bash
bash Tools/Outline.sh list "<email>" "<collection_id>" 100
```

If the user has no access to that collection, the call exits non-zero — surface the error to the user instead of retrying with a different account.

## Error handling

The script exits non-zero with a stderr message for these cases:

| Exit | Meaning                                  | What to tell the user                        |
|------|------------------------------------------|----------------------------------------------|
| 2    | Missing required argument                | Ask the agent for the missing field          |
| 3    | `users.list` API call failed             | Check Outline status / token validity        |
| 4    | Email not found in workspace             | "<email> is not a member of this workspace"  |
| 5    | User is suspended                        | "<email> is suspended; access denied"        |
| 6    | `collections.list` failed                | Check Outline status / token validity        |
| 7    | User lacks permission for that resource  | "<email> cannot view this doc/collection"    |

Always check `.ok` on raw curl output if you ever bypass the script. Outline returns `{"ok": false, "error": "..."}` on failure.

## Pagination

The script defaults to `limit=10` for search and `limit=100` for collection listings. Outline caps `limit` at 100 — for larger result sets, call `documents.list` directly with `offset` increments via the same auth pattern (still post-filter against `visible_collection_ids` for the requesting user).

## See also

- `OutlineLocal` — same Outline endpoints but with no per-user filtering. Use that one when you don't need workspace-permission scoping (e.g. a workspace where everyone has the same Outline access, or when you want lower latency by skipping the 3 extra API calls).
