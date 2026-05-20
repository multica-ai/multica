---
name: outline-local
description: Read-only access to the self-hosted Outline at docs.zoop.tools using only OUTLINE_API_KEY. No per-user filtering, no email arg — returns whatever the workspace admin token can see. USE WHEN search outline, find in outline, look up outline, get outline doc, list outline collections, list outline documents, outline knowledge base AND per-user permission scoping is not needed.
---

# OutlineLocal (Read-Only, Admin-Token, No Filtering)

Minimal read-only client for `https://docs.zoop.tools`. Uses `OUTLINE_API_KEY` directly with no per-user filtering and no email argument. Whatever the admin token can see is what the caller gets.

This is the **simpler sibling** of the `Outline` skill. Pick this one when:

- The workspace doesn't need per-user permission scoping (everyone has the same Outline access).
- You want lower latency: this skill makes 1 HTTP call per request; `Outline` makes 4 (resolve user, list memberships, list groups, then the actual call).
- You're testing the Outline integration without involving the Multica trigger-user resolution chain.

If per-user filtering is required, use the **`Outline`** skill instead.

## Hard rules

- **Read-only.** Never call write endpoints (`documents.create/update/delete/move`, etc.). If the user asks for an edit, respond:
  > This skill is read-only. Make the change in Outline directly at https://docs.zoop.tools.
- **Never print `OUTLINE_API_KEY`.** Keep the literal `$OUTLINE_API_KEY` reference in any command shown to the user.
- **No filtering.** This skill returns everything the admin token can see. Do NOT use it in workspaces where per-user scoping matters — use the `Outline` skill there.

## Prerequisite

`OUTLINE_API_KEY` must be set in the environment. Verify before any call:

```bash
[ -n "$OUTLINE_API_KEY" ] && echo "ok" || echo "missing"
```

If missing, stop and tell the user:
> `OUTLINE_API_KEY` is not set. Generate an **admin** token at `https://docs.zoop.tools/settings/api-tokens` and run `export OUTLINE_API_KEY="<token>"` (or set it in the agent's Custom Env / daemon container env when running in Multica).

## Helper script

All API calls go through `Tools/OutlineLocal.sh`:

```bash
bash Tools/OutlineLocal.sh <subcommand> ...
```

## Workflows

### 1. Search documents
**Triggers:** "search outline for X", "find X in outline", "look up X in our docs"

```bash
bash Tools/OutlineLocal.sh search "<query>" 10
```

Returns up to 10 matches across the entire workspace, each with `title`, `url`, `snippet`.

### 2. Get a document by ID or slug
**Triggers:** "show me the X doc", "get outline doc <id>", "open <slug> from outline"

```bash
bash Tools/OutlineLocal.sh get "<doc_id_or_slug>"
```

Returns `{title, url, text}` (full markdown body). `id` accepts UUID or URL slug (e.g. `engineering-handbook-AbCdEf`).

### 3. List collections
**Triggers:** "list outline collections", "what collections do we have", "show outline workspaces"

```bash
bash Tools/OutlineLocal.sh collections
```

Returns every collection in the workspace.

### 4. List documents in a collection
**Triggers:** "list docs in <collection>", "show me everything in <collection>"

Resolve the collection ID via workflow 3 first if the user gave a name, then:

```bash
bash Tools/OutlineLocal.sh list "<collection_id>" 100
```

## Error handling

| Exit | Meaning                       | What to tell the user                  |
|------|-------------------------------|----------------------------------------|
| 1    | `OUTLINE_API_KEY` not set     | Set the env var; see prerequisite     |
| 2    | Missing required argument     | Ask the agent for the missing field   |
| 3    | API call returned `ok=false`  | Check Outline status / token validity |

Outline returns `{"ok": false, "error": "..."}` on failure. The script surfaces `error` on stderr.

## Pagination

Defaults: `limit=10` for search, `limit=100` for collection listings. Outline caps `limit` at 100. For larger result sets, call the underlying endpoints (`documents.search`, `documents.list`) directly with `offset` increments.

## See also

- `Outline` — same Outline endpoints with **per-user filtering** layered on top. Inside Multica, it auto-resolves the triggering user's email from `MULTICA_TASK_ID` and post-filters results to that user's collection / group / document memberships.
