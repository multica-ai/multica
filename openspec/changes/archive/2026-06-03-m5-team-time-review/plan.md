# M5 Implementation Plan

STATUS: USER APPROVED — implementing directly.

## Step 1 — Backend SQL

File: `server/pkg/db/queries/time_entry.sql`

Add three new queries:
- `SumTimeEntriesByUserInWorkspace` (:many)
- `SumTimeEntriesByProjectInWorkspace` (:many)
- `SumTimeEntriesForProject` (:one)

## Step 2 — Run `make sqlc`

Regenerates `server/pkg/db/generated/time_entry.sql.go`.

## Step 3 — Backend handler

File: `server/internal/handler/time_entry.go`

Add:
- `TeamTimeStatsResponse` struct
- `GetTeamTimeStats` handler

File: `server/internal/handler/project.go` (or similar)

Add:
- `GetProjectTimeStats` handler

## Step 4 — Register routes

File: `server/cmd/server/router.go`

- `r.Get("/team-stats", h.GetTeamTimeStats)` inside `/api/time-entries` group
- `r.Get("/time-stats", h.GetProjectTimeStats)` inside `/api/projects/{id}` group

## Step 5 — Frontend types

File: `apps/workspace/src/shared/types/time-entry.ts`

Add `TeamTimeStats`, `TeamTimeUserStat`, `TeamTimeProjectStat`.

## Step 6 — API client

File: `apps/workspace/src/shared/api/client.ts`

Add `getTeamTimeStats(params)` and `getProjectTimeStats(projectId)`.

## Step 7 — Query keys

File: `apps/workspace/src/shared/query/keys.ts`

Add `timeTracking.teamStats` and `projects.timeStats`.

## Step 8 — Hooks

File: `apps/workspace/src/features/time-tracking/hooks/use-time-tracking.ts`

Add `useTeamTimeStatsQuery`.

File: `apps/workspace/src/features/projects/queries.ts`

Add `useProjectTimeStatsQuery`.

## Step 9 — TeamTimePage

File: `apps/workspace/src/features/time-tracking/pages/TeamTimePage.tsx`

New page with range selector + member table + project table.

## Step 10 — Project detail time spent

File: `apps/workspace/src/features/projects/components/projects-page.tsx`

Add one "Time spent: X h Y m total" stat row inside `ProjectDetailPanel`.

## Step 11 — Router + Nav

Files: `apps/workspace/src/router.tsx`, `apps/workspace/src/features/layout/navigation.ts`

Add `/team-time` route and "Team Time" nav item.

## Step 12 — Export index

File: `apps/workspace/src/features/time-tracking/index.ts`

Export `TeamTimePage`.

## Step 13 — Verify

Run `pnpm typecheck` and `make test`.
