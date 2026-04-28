# Tech Spec: Cross-Workspace Meta View

- Companion to: `0001-cross-workspace-meta-view.md`
- Date: 2026-04-28
- Author: Ultron CTO

This document is the implementation contract. Read the ADR first for the
"why"; this document is for "what exactly to build".

## 1. Backend — `GET /api/issues/cross-workspace`

### 1.1. Path & auth

- Path: `GET /api/issues/cross-workspace`
- Auth: standard session auth (the same `middleware.Auth(queries)` chain that
  protects every authenticated route).
- **Not** under the `RequireWorkspaceMember` group, because there is no
  workspace ID in the URL. Membership is enforced inside the SQL query.

### 1.2. Query parameters

| Param           | Type     | Default        | Notes |
|-----------------|----------|----------------|-------|
| `status`        | string   | (none)         | Comma-separated list. Allowed: `backlog`, `todo`, `in_progress`, `in_review`, `done`. Multiple = OR. |
| `priority`      | string   | (none)         | Comma-separated. `low` `medium` `high` `urgent`. |
| `assignee_id`   | uuid     | (none)         | Single assignee. |
| `assignee_ids`  | string   | (none)         | Comma-separated UUIDs. |
| `workspace_ids` | string   | (none)         | Comma-separated workspace UUIDs. Server intersects with membership; unknown IDs are silently dropped. |
| `limit`         | int      | `50`           | Hard cap `100`. Values above are clamped, not rejected. |
| `after`         | string   | (none)         | Opaque cursor; base64-encoded `created_at|id`. |
| `open_only`     | bool     | `false`        | If `true`, ignores `status` and returns everything except `done` and `cancelled`. |

### 1.3. Response shape

200 OK:

```json
{
  "issues": [
    {
      "id": "uuid",
      "identifier": "MUL-3",
      "number": 3,
      "title": "Design cross-workspace meta view",
      "description": "...",
      "status": "in_progress",
      "priority": "high",
      "assignee_id": "uuid",
      "assignee_type": "agent",
      "creator_id": "uuid",
      "creator_type": "member",
      "parent_issue_id": null,
      "project_id": null,
      "position": 0,
      "due_date": null,
      "created_at": "2026-04-28T02:43:32Z",
      "updated_at": "2026-04-28T02:43:32Z",
      "labels": [],
      "workspace": {
        "id": "uuid",
        "name": "Multica Fork",
        "slug": "multica-fork",
        "issue_prefix": "MUL",
        "color": "#7c3aed"
      }
    }
  ],
  "next_cursor": "eyJjcmVhdGVkX2F0Ijoi..." ,
  "has_more": true,
  "total_returned": 50
}
```

- `next_cursor` is `null` when there are no more results.
- `total_returned` is the count of items in `issues` (not a global total — we
  intentionally do NOT compute a global `COUNT(*)` for the cross-workspace
  query because of cost; the global view shows "showing N issues" instead of
  "N of M").

### 1.4. Error cases

| Status | When | Body |
|--------|------|------|
| `401`  | unauthenticated | `{"error":"unauthorized"}` |
| `400`  | malformed `after` cursor or unknown `status` value | `{"error":"<detail>"}` |
| `500`  | DB error | `{"error":"failed to list cross-workspace issues"}` (logs include trace ID) |

A user with **zero** workspaces returns `200 { issues: [], next_cursor: null,
has_more: false }` — empty, not 404.

### 1.5. SQL (sqlc)

A new query `ListCrossWorkspaceIssues` in `server/internal/queries/issue.sql`:

```sql
-- name: ListCrossWorkspaceIssues :many
SELECT i.*, w.id   AS w_id,
            w.name AS w_name,
            w.slug AS w_slug,
            w.issue_prefix AS w_prefix
FROM issue i
JOIN workspace w ON w.id = i.workspace_id
JOIN member    m ON m.workspace_id = i.workspace_id
WHERE m.user_id = $1
  AND ($2::uuid[]   IS NULL OR i.workspace_id = ANY($2))
  AND ($3::text[]   IS NULL OR i.status       = ANY($3))
  AND ($4::text[]   IS NULL OR i.priority     = ANY($4))
  AND ($5::uuid[]   IS NULL OR i.assignee_id  = ANY($5))
  AND ($6::timestamptz IS NULL OR (i.created_at, i.id) < ($6, $7))
ORDER BY i.created_at DESC, i.id DESC
LIMIT $8;
```

The cursor `($6, $7)` uses `(created_at, id)` keyset semantics. We request
`limit + 1` rows internally; if we get back `limit + 1` we set
`has_more = true` and drop the last one.

### 1.6. Color derivation

`server/internal/util/wscolor.go`:

```go
var palette = []string{
  "#ef4444", "#f97316", "#f59e0b", "#eab308",
  "#84cc16", "#22c55e", "#10b981", "#14b8a6",
  "#06b6d4", "#3b82f6", "#8b5cf6", "#ec4899",
}

func WorkspaceColor(id uuid.UUID) string {
  // FNV-1a 32-bit hash over the UUID bytes, modulo palette length.
  ...
}
```

### 1.7. Tests (Tony)

- Happy path: user with 3 workspaces, ~50 issues each. Response merges all,
  sorted by `created_at DESC`. Pagination across the boundary is consistent
  (no duplicates, no skips).
- Empty path: user with zero workspaces returns `[]`.
- Filter intersection: caller passes `workspace_ids` containing one workspace
  they belong to and one they do not. Result includes only issues from the
  one they belong to. Status code is `200`, not `403`.
- Auth: unauthenticated returns `401`.
- Cursor: malformed `after` returns `400`.

## 2. Frontend

### 2.1. Routes

```
apps/web/app/
├── global/
│   ├── layout.tsx       <- WorkspaceRail + page content (no workspace dropdown)
│   └── page.tsx         <- Cross-workspace Kanban
└── [workspaceSlug]/
    └── (dashboard)/
        └── layout.tsx   <- WorkspaceRail + workspace-scoped sidebar (existing)
```

- The `<WorkspaceRail />` is hoisted into both layouts so it is always
  visible.
- `apps/web/app/global/page.tsx` is a client component that reads filters
  from URL search params (`?status=...&priority=...`).
- Add `global` to `workspaceReservedSlugs` so a workspace can never have the
  slug `global`.

### 2.2. Component tree

```
<WorkspaceRail />
├── <RailItem href="/global" icon={GlobeIcon} label="All workspaces" />
├── <RailItem href="/<slug>" avatar={ws.avatar_url|color} label={ws.name} /> * N
└── <RailItem onClick={openCreateWorkspaceModal} icon={PlusIcon} label="New" />

<GlobalKanbanPage />
├── <GlobalKanbanFilters />          <- workspace multi-select, assignee, priority, status
├── <GlobalKanbanBoard>
│   ├── <KanbanColumn status="backlog">
│   │   └── <CrossWorkspaceIssueCard /> * N
│   ├── <KanbanColumn status="todo"> ...
│   └── ... (5 columns)
└── <GlobalKanbanFooter />           <- "Showing N issues. Load more"
```

### 2.3. Reused vs new components

- **Reused:** `<KanbanColumn />`, `<IssueCard />` (extended via prop, see
  below), the existing filter primitives (`<MultiSelect />`, etc.).
- **New:**
  - `packages/views/workspace/workspace-rail.tsx`
  - `packages/views/issues/components/workspace-badge.tsx` — small chip
    rendering `{color, prefix}`. Used only inside the cross-workspace card.
  - `packages/views/issues/global-kanban/index.tsx` — orchestrates the
    cross-workspace view. Reuses `<KanbanColumn />`.

The existing `<IssueCard />` gains an optional `workspaceBadge?: ReactNode`
prop. Default-undefined keeps existing call sites unchanged.

### 2.4. State management

- React Query key: `["issues", "cross-workspace", normalizedFilters]`.
- Mutations from anywhere in the app that touch issues already invalidate
  per-workspace keys. We extend the invalidator in
  `packages/core/issues/mutations.ts` to also invalidate the
  `["issues", "cross-workspace"]` key family.
- `useCrossWorkspaceIssues(filters)` in `packages/core/issues/queries.ts`
  wraps the new endpoint, mirroring `useIssues(workspaceId, filters)`.

### 2.5. Realtime

- The realtime hub already publishes per-workspace channels. The global page
  subscribes to every channel for workspaces in the user's
  `listWorkspaces()` response.
- On any `issue.updated` / `issue.created` / `issue.deleted` event from any
  of those channels, invalidate
  `["issues", "cross-workspace"]`.

### 2.6. Filters

- **Workspace multi-select:** the user can narrow the global view to a
  subset of their workspaces. Selected IDs feed into `?workspace_ids=`.
- **Assignee:** people + agents. Server filters via `assignee_ids`. Workspace
  agents do not span workspaces, so the dropdown lists members + agents
  across the union of selected workspaces (or all workspaces if none
  selected).
- **Priority, Status:** identical to the per-workspace page.
- All filter state is mirrored into URL search params for shareable links.

### 2.7. Empty / loading / error UX

- Loading: skeleton columns (existing `<KanbanSkeleton />`).
- Empty: a polite "No issues across your workspaces yet" with a button to
  create an issue (which opens the existing modal scoped to a workspace
  picker).
- Error: existing `<ErrorBoundary />` with a "Retry" button.

## 3. Migrations / DB

**None in v1.** The endpoint is read-only over existing tables (`issue`,
`workspace`, `member`). The deterministic color helper requires no schema
change.

If a v2 ever needs persistent colors:

```sql
ALTER TABLE workspace ADD COLUMN color TEXT;
```

— and the helper falls back to the deterministic value when the column is
NULL. Out of scope for v1.

## 4. Telemetry / observability

- Structured log per request:
  - `endpoint=/api/issues/cross-workspace`
  - `user_id=...`
  - `workspace_count=...` (how many workspaces the user belongs to)
  - `filter_workspace_ids=...`
  - `result_count=...`
  - `duration_ms=...`
- Prometheus histogram `multica_cross_workspace_issues_duration_ms`.
- Alert (JARVIS, sub-issue #6): page if p95 over 5 minutes > 250ms.
- The frontend reports a single analytics event `cross_workspace_view` with
  the filter set (workspace count, status filter active y/n) on first paint.

## 5. Rollout & flags

- v1 ships behind a feature flag `feature.cross_workspace` (config), default
  `true` for self-host (where Cuong is the only user that matters), default
  `false` for cloud until we have load numbers from self-host.
- The feature flag gates both the rail and the route. When off, `/global`
  returns 404 and `<WorkspaceRail />` renders as a no-op (the existing
  workspace dropdown stays the only switcher).

## 6. Acceptance checklist (for the parent issue MUL-3)

- [ ] ADR `0001-cross-workspace-meta-view.md` merged.
- [ ] Companion tech spec merged in the same PR.
- [ ] 5 sub-issues created and parent-linked to MUL-3.
- [ ] Sub-issue #2 (Tony) has the API contract above as its description.
- [ ] Sub-issue #3 (Peter) has the rail component contract.
- [ ] Sub-issue #4 (Peter) has the global Kanban contract.
- [ ] Sub-issue #5 (Bruce) has the E2E coverage list.
- [ ] Sub-issue #6 (JARVIS) has the deploy + observability contract.
