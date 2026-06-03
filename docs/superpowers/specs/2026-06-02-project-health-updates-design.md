# Project Health & Updates — Design

**Date:** 2026-06-02
**Status:** Approved (Phase 1 to build now; Phase 2 as fast-follow)

## Problem

Multica projects today only surface issue-derived data (status, priority, lead,
issue counts, board/list/gantt views). They lack Linear-style **project health**
and **project updates** — a narrative, human/agent-authored signal about how a
project is *actually* going, independent of issue state.

Moses wants the Linear experience: a project lead (or agent) posts periodic
updates, each carrying a health status, and the project's current health is the
health of its most recent update.

## Decisions (from brainstorming)

- **Health model:** Tied to updates (Linear-style). A project's current health =
  the most recent update's `health`. No separate, independently-settable health
  field.
- **Authors:** Members *and* agents. Agents render with the existing purple /
  robot styling already used for agent assignees.
- **Surfacing:** Project detail tab only for now. No workspace-wide activity feed
  in this work.
- **Dates:** Add `start_date` + `target_date` to projects so "on track" has a
  timeline to reference.
- **Update content:** Rich markdown body (reuse the existing `ContentEditor`) +
  required health. Comments/reactions on updates are deferred to Phase 2.

## Scope

### Phase 1 — Core (build now)
Health, updates, and project dates. This is the heart of the request and mirrors
the existing `project_resource` sub-collection pattern end-to-end.

### Phase 2 — Engagement (fast-follow, separate spec/plan)
Comments + reactions on updates. Deferred because the existing comment/reaction
systems are **hard-wired to issues** (`comment.issue_id NOT NULL`; separate
`comment_reaction` / `issue_reaction` tables — the codebase pattern is one table
per entity, not polymorphic). Adding them to updates requires new
`project_update_comment` + `project_update_reaction` tables, parallel
handlers/queries, and generalizing the frontend `CommentCard` (`issueId` →
`entityId`/`entityType`) plus extracting a generic `useEntityReactions` hook.
That cost is isolated here so Phase 1 ships cleanly.

---

## Phase 1 Architecture

### Data model

**Alter `project`** (new migration):
- `start_date DATE NULL`
- `target_date DATE NULL`

**New table `project_update`** (new migration):

```sql
CREATE TABLE project_update (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    project_id   UUID NOT NULL REFERENCES project(id) ON DELETE CASCADE,
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    health       TEXT NOT NULL CHECK (health IN ('on_track','at_risk','off_track')),
    body         TEXT NOT NULL DEFAULT '',
    author_type  TEXT NOT NULL CHECK (author_type IN ('member','agent')),
    author_id    UUID NOT NULL,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_project_update_project   ON project_update(project_id, created_at DESC);
CREATE INDEX idx_project_update_workspace ON project_update(workspace_id);
```

Workspace-scoped everywhere (FK + WHERE-clause guard), per repo convention.

### Derived health on the project

`ProjectResponse` (in `server/internal/handler/project.go`) gains:
- `health: string | null` — health of the latest update (null if no updates yet)
- `last_update_at: string | null` — timestamp of the latest update
- `start_date` / `target_date`

`ListProjects` batch-loads the latest update per project in one query (mirroring
the existing batched `GetProjectIssueStats` / resource-count approach — no N+1),
so the projects list can render a health dot without fetching update bodies.

### Backend (mirror `project_resource` end-to-end)

- **Queries** `server/pkg/db/queries/project_update.sql`:
  `ListProjectUpdates`, `CreateProjectUpdate`, `GetProjectUpdate`,
  `UpdateProjectUpdate`, `DeleteProjectUpdate`, plus
  `GetLatestUpdatesForProjects` (batch, for the list view). Run `make sqlc`.
- **Handler** `server/internal/handler/project_update.go`: `ProjectUpdateResponse`
  struct + CRUD handlers. Resolve the project via the existing project loader,
  use `entity.ID` for writes, parse the `{updateId}` path param with
  `parseUUIDOrBadRequest`. Validate `health` and `author_type` at the boundary.
- **Routes** in `server/cmd/server/router.go`, nested under the existing
  `/api/projects/{id}` group:
  ```
  /api/projects/{id}/updates
    GET   ListProjectUpdates
    POST  CreateProjectUpdate
    /{updateId}
      GET    GetProjectUpdate
      PUT    UpdateProjectUpdate
      DELETE DeleteProjectUpdate
  ```
- **WS events** in `server/pkg/protocol/events.go`:
  `project_update:created|updated|deleted`. Publish from the handlers.
- `UpdateProject` / `CreateProject` extended to accept the new `start_date` /
  `target_date` fields.

### Frontend

**Types** (`packages/core/types/project.ts`):
- `ProjectHealth = "on_track" | "at_risk" | "off_track"`
- `ProjectUpdate`, `CreateProjectUpdateRequest`, `UpdateProjectUpdateRequest`
- Extend `Project` with `health?: ProjectHealth | null`, `last_update_at?`,
  `start_date?`, `target_date?`. Extend create/update requests with the date
  fields.

**API client** (`packages/core/api/client.ts`): `listProjectUpdates`,
`createProjectUpdate`, `updateProjectUpdate`, `deleteProjectUpdate`. Each parsed
through a zod schema via `parseWithFallback` per the API Response Compatibility
rule, with a malformed-response test.

**Query layer** (`packages/core/projects/queries.ts` + `mutations.ts`):
- `projectUpdateKeys` (workspace + project scoped) and
  `projectUpdatesListOptions(wsId, projectId)`.
- Optimistic `useCreateProjectUpdate` / `useUpdateProjectUpdate` /
  `useDeleteProjectUpdate`; invalidate both the updates list *and* the project
  detail/list (so the derived health dot refreshes).

**Realtime** (`packages/core/realtime/use-realtime-sync.ts`): on
`project_update:*`, invalidate the project-updates queries for the workspace and
the project list (to refresh derived health).

**UI** (`packages/views/projects/`):
- `health-pill.tsx` (in `packages/ui` if purely presentational): green/amber/red
  pill + label, driven by `ProjectHealth`. Uses semantic tokens, not hardcoded
  colors. Shown in the project header and the projects list rows.
- `project-update-card.tsx`: one update — author (member/agent purple styling via
  existing assignee rendering), relative time, health pill, rendered markdown
  body, plus edit/delete affordances for the author/moderator.
- `project-update-composer.tsx`: `ContentEditor` body + health selector; calls
  `useCreateProjectUpdate`.
- `project-updates-tab.tsx`: composer + reverse-chronological feed; empty state.
- Wire a new **Updates** view into the project detail view switcher alongside
  Board / List / Gantt / Swimlane.
- Project header sidebar: render `start_date → target_date` with a "X days left"
  helper, and the current health pill.

Components live in `packages/views` (shared web + desktop); no `next/*` or
`react-router-dom` imports; navigation via `useNavigation()`.

### Error handling & edge cases

- Project with **no updates**: `health`/`last_update_at` are null; list shows a
  neutral "No update" indicator; tab shows an empty state prompting the first
  update.
- **Enum drift:** an unknown `health` value renders a neutral fallback pill
  (switch has a `default` branch) rather than crashing — per repo enum-drift rule.
- **Dates:** `target_date` before `start_date` is allowed but the "days left"
  helper guards against negative/None; missing dates simply hide the helper.
- Deleting the latest update recomputes derived health to the prior update (or
  null). Achieved naturally by the batched latest-update query.

### Testing

- **Go:** handler tests for project_update CRUD with workspace isolation
  (cross-workspace access denied), latest-update derivation in `ListProjects`,
  and author_type/health validation.
- **`packages/core`:** schema tests feeding malformed update responses through
  `parseWithFallback`; query-key + mutation invalidation tests.
- **`packages/views`:** render tests for the updates tab (feed, empty state),
  health pill enum-drift fallback, and composer submit.
- No E2E required for Phase 1 (can add later); manual `make check` before done.

## Non-goals (Phase 1)

- Comments and reactions on updates (Phase 2).
- Workspace-wide activity feed / inbox surfacing of updates.
- Auto-computed health from issue progress.
- Scheduled/recurring update reminders.

## Build order

1. Migrations (`project` dates + `project_update` table) → `make sqlc`.
2. Backend queries + handler + routes + WS events; Go tests.
3. Core types + API client (+ schemas) + query/mutation layer + realtime; core tests.
4. Views: health pill, update card, composer, updates tab, header dates; view tests.
5. `make check`.
