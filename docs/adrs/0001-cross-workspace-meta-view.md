# ADR 0001: Cross-Workspace Meta View

- Status: Proposed
- Date: 2026-04-28
- Author: Ultron CTO
- Driver: Cuong (owner, multi-business operator)

## Context

Multica self-host (`team.cuongpho.com`) is being used to pilot AI agents across
multiple businesses. The owner currently belongs to 6 active workspaces:

| Workspace            | ID                                     |
| -------------------- | -------------------------------------- |
| Fuchsia B2B          | `42a4a362-b06c-4e64-b6a2-f9e73c67af63` |
| Fuchsia B2C          | `43725562-2f1e-45e7-aa21-5d79c7a8f5d5` |
| Business             | `facace57-fbeb-4839-a070-918e29a482cb` |
| The Institut Academy | `34225cad-856f-4212-b87a-aeca9c4ea002` |
| Tiny Little Space    | `0f5f7dc3-a221-4d61-9e2c-05ba2926ed68` |
| Multica Fork         | `6940266a-b0d5-43e2-b63c-d0f9b091e11f` |

Today the only way to switch context is the workspace dropdown in the existing
sidebar. There is no place where the operator can see, in one glance, what
every agent across every business is currently working on.

The product goal is a **cross-workspace meta view**:

1. A persistent thin left rail (`~56px`) listing every workspace the user is a
   member of, plus a special "All workspaces" entry pinned at the top.
2. A new top-level route showing a giant Kanban that aggregates issues from
   every workspace the user belongs to, in the standard 5 columns
   (Backlog / Todo / In Progress / In Review / Done).
3. Each issue card shows a workspace badge so the source workspace is visible
   at a glance.
4. Filters: workspace (multi-select), assignee, priority, status.

This ADR captures the architecture decisions; the companion document
`0001-cross-workspace-meta-view-techspec.md` captures the concrete contracts.

## Decision summary

| # | Decision | Choice |
|---|---|---|
| D1 | Aggregation strategy | New backend endpoint `GET /api/issues/cross-workspace` |
| D2 | Permissions model | Server-side filter to workspaces where the caller is a `member` row |
| D3 | Data shape | Existing `IssueResponse` enriched with embedded `workspace` object |
| D4 | Pagination | Keyset pagination on `(created_at DESC, id DESC)`, page size 50, hard cap 100 |
| D5 | URL routing | `/global` (top-level, outside `[workspaceSlug]`) |
| D6 | Sidebar layout | New `<WorkspaceRail />` always-visible thin rail. Existing dropdown stays inside the workspace-scoped sidebar |
| D7 | Workspace color | Derived deterministically from workspace ID (no DB migration in v1) |
| D8 | Group-by | `status` only in v1. `workspace` group-by deferred to v2 |
| D9 | Cache & realtime | Reuse the existing realtime hub; tag query keys per-workspace so a single workspace mutation invalidates only its slice |

## Decisions in detail

### D1. Aggregation strategy — backend endpoint

**Choice:** add a new `GET /api/issues/cross-workspace` handler that
aggregates issues across all workspaces the caller is a member of, in a single
SQL query.

**Why:**

- One round-trip instead of N (N = number of workspaces the user is a member
  of). With 6 workspaces today, the frontend would otherwise issue 6 parallel
  `ListIssues` requests; with growth this will not scale.
- Auth check runs once on the server using the same `member` table that the
  rest of the app already uses. No risk of "I forgot to filter" leaks on the
  client.
- Pagination becomes a single problem (one cursor) instead of N interleaved
  problems on the frontend.
- Server can apply the workspace, assignee, priority, status filters in SQL
  with one composable query — much cheaper than fan-out + merge in JS.
- We can add cross-workspace ordering (created_at DESC) consistently. With the
  per-workspace approach the frontend would have to re-sort the merged list
  every time more pages stream in.

**Rejected alternative — frontend fan-out:** issuing N parallel calls to the
existing `ListIssues` endpoint and merging client-side. Wins: zero backend
work. Loses: chatty, awkward pagination, sort merging, no single source of
truth for "across my workspaces".

### D2. Permissions model

**Rule:** the cross-workspace endpoint returns issues only from workspaces
where the authenticated user has a row in `member` (any role: `owner`, `admin`,
`member`). Same membership semantics as the rest of the app.

- The existing `RequireWorkspaceMember` middleware does not apply here (it
  takes a workspace ID from the URL and checks membership for that one
  workspace).
- Instead, the new handler will resolve the caller's workspace IDs from the
  `member` table inside the SQL query itself (`WHERE workspace_id IN (SELECT
  workspace_id FROM member WHERE user_id = $1)`), so there is no round-trip
  between auth resolution and the data query.
- The `?workspace_ids=...` filter, when supplied, is intersected with that
  membership set on the server. A user passing a workspace ID they do not
  belong to silently gets it filtered out (no 403 — we don't want to leak
  whether a workspace exists).

### D3. Data shape

Each issue in the response uses the existing `IssueResponse` enriched with an
embedded `workspace` block:

```json
{
  "id": "uuid",
  "identifier": "MUL-3",
  "title": "Design cross-workspace meta view",
  "status": "in_progress",
  "priority": "high",
  "assignee_id": "uuid",
  "assignee_type": "agent",
  "created_at": "2026-04-28T02:43:32Z",
  "updated_at": "2026-04-28T02:43:32Z",
  "workspace": {
    "id": "6940266a-b0d5-43e2-b63c-d0f9b091e11f",
    "name": "Multica Fork",
    "slug": "multica-fork",
    "issue_prefix": "MUL",
    "color": "#7c3aed"
  }
}
```

Notes:

- `workspace.color` is derived server-side from the workspace UUID using a
  deterministic hash (HSL palette of 12 colors). No DB migration in v1 — see
  D7.
- The rest of the issue fields stay byte-identical to `IssueResponse` so the
  frontend can reuse existing card components.

### D4. Pagination & performance

- Default page size: `50`. Hard server cap: `100`.
- Default sort: `created_at DESC, id DESC` (stable tie-breaker).
- Cursor: `?after=<base64(created_at|id)>`. Keyset, not offset — keeps query
  cost flat as the user scrolls.
- Response shape: `{ issues: [...], next_cursor: "..." | null, has_more: bool }`.
- Existing per-workspace `ListIssues` already takes ~50ms for ~200 issues; the
  cross-workspace variant adds one extra `WHERE workspace_id IN (...)` clause.
  Expected p95 < 100ms for the workspace counts we see today (≤ 6 workspaces,
  ≤ ~2k issues total).
- The existing `(workspace_id, created_at)` index already covers the access
  pattern; no new indexes needed in v1. JARVIS adds a slow-query alert if p95
  drifts past 250ms.

### D5. URL routing

**Choice:** `/global`.

- Short, single-word, no collision with existing `[workspaceSlug]` segment
  because `global` is reserved.
- `/all` is too generic and could be misread as "all of something inside one
  workspace".
- `/workspaces/all` reads like a workspaces admin page; the meta view is about
  issues, not workspaces.

The route lives outside the `[workspaceSlug]` Next.js segment, in
`apps/web/app/global/`, so it has its own layout that hosts the workspace rail
without the per-workspace sidebar.

The reserved-slug list (`apps/web/app/[workspaceSlug]` already excludes
`auth`, `landing`, etc. via `workspaceReservedSlugs`) gets `global` added.

### D6. Sidebar layout impact

Two surfaces, side by side:

```
+----+--------------------------+----------------------------------+
| WR | Workspace sidebar (slug) | Page content                     |
| .. | (issues / chat / ...)    |                                  |
| WR |                          |                                  |
| WR |                          |                                  |
+----+--------------------------+----------------------------------+
```

- `WR` = `<WorkspaceRail />`, ~56px wide, always visible (including on the
  global view).
- The existing workspace-scoped sidebar (Inbox, Chat, My Issues, Issues, ...)
  stays as is — it is workspace-local and only renders inside the
  `[workspaceSlug]` segment.
- On the global view, only the rail + the global page render. The
  workspace-scoped sidebar is absent.

The rail entries (top to bottom):

1. "All" — links to `/global`. Active when pathname starts with `/global`.
2. One avatar per workspace the user is a member of. Active when the current
   pathname's first segment matches that workspace slug.
3. A `+` button at the bottom — opens the existing "create workspace" modal.

The rail replaces the *workspace switching* role of the dropdown that lives
inside the existing sidebar header. The dropdown stays for now but becomes
secondary; we will revisit removing it in a follow-up once the rail proves
itself.

ASCII reference:

```
+---+
| A |  <- "All workspaces" button (filled when on /global)
|---|
| F |  <- Fuchsia B2B avatar
|   |
| F |  <- Fuchsia B2C
|   |
| B |  <- Business
|   |
| T |  <- The Institut Academy
|   |
| T |  <- Tiny Little Space
|   |
| M |  <- Multica Fork
|---|
| + |
+---+
```

### D7. Workspace color

The `workspace` table has no `color` column today. Two options:

- **(a) Add a column** via a migration. Wins: persistent, user-customizable.
  Loses: schema change, migration coordination, one more thing to seed.
- **(b) Derive deterministically** from the workspace UUID using a 12-entry
  HSL palette. Wins: zero migration, zero seeding, immediate. Loses: not
  user-customizable.

**Choice for v1: (b) deterministic derivation, server-side.** Customization
becomes a v2 concern; do (a) only if Cuong asks for it.

The derivation lives in a small `internal/util/wscolor.go` helper so backend
and any future server-rendered surface stay consistent. The frontend trusts
the server-provided `color` field; no client-side recomputation.

### D8. Group-by

- v1: `status` group-by only. Five columns Backlog / Todo / In Progress /
  In Review / Done — exactly the columns of the existing per-workspace board.
- v2 (deferred, not in this ADR's scope): a toggle to group by `workspace`
  (one column per business). Useful when the operator wants to see "what's
  Tiny Little Space currently doing" in isolation, without leaving the meta
  view. Document this as a future enhancement; do not build it yet.

### D9. Cache & realtime

- The frontend already keys React Query cache by workspace ID. We add a new
  key `["issues", "cross-workspace", filters]` for the global view.
- On any issue mutation (create/update/delete) we already publish to the
  realtime hub on a workspace-scoped channel. The global view subscribes to
  every workspace channel the user is a member of and invalidates the
  cross-workspace query key on any event.
- No new database triggers, no new event types. The cost is one additional
  websocket subscription per workspace on this single page — negligible.

## Consequences

### Positive

- Single round-trip for the meta view. Predictable performance.
- Strict server-side membership filter — no risk of leaking issues across
  tenant boundaries.
- The data shape is a strict superset of `IssueResponse`, so existing card
  components reuse cleanly.
- No DB migration in v1; the change is purely additive (new endpoint, new
  routes, new components).

### Negative / costs

- The deterministic color (D7) means workspaces cannot pick a custom color
  until v2.
- The workspace rail adds ~56px of permanent horizontal chrome. We accept
  this; the existing dropdown is going to feel redundant for a while.
- Realtime invalidation on the global view subscribes to N workspace channels
  in parallel. With 6 workspaces this is fine; if a user belongs to dozens we
  will need to revisit.

### Out of scope

- Editing or rearranging issues from the global view (drag between columns,
  drag between workspaces). v1 is read + filter only. Clicking a card
  navigates to that issue inside its workspace.
- Per-user persisted filter state (saved views, bookmarked filters).
- Bulk actions across workspaces.

## Open questions

1. Should "All workspaces" appear at the top or bottom of the rail? Choosing
   top in v1 because that mirrors the natural "overview-then-detail" reading
   order; revisit if Cuong's muscle memory disagrees.
2. Should the global Kanban paginate per column, or globally? Going with
   globally for simplicity; per-column pagination is a v2 stretch.

## References

- Companion tech spec: `0001-cross-workspace-meta-view-techspec.md`
- Existing per-workspace endpoint: `server/internal/handler/issue.go::ListIssues`
- Existing sidebar: `packages/views/layout/app-sidebar.tsx`
- Reserved slug list: `server/internal/handler/workspace_reserved_slugs.go`
