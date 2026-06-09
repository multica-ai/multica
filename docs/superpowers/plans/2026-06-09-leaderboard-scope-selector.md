# Leaderboard Scope Selector (M-31)

## Goal

Add a 3-way scope toggle (Agent / Model / Runtime) to the workspace dashboard Leaderboard, letting users see token spend by model and run-time by runtime alongside the existing per-agent view.

## Split recommendation: no split

~150 LOC backend delta + shared TS types = one branch.

## Tasks

| # | Layer | Files |
|---|---|---|
| 1 | SQL | `server/pkg/db/queries/task_usage.sql` — add `ListDashboardUsageByModel` |
| 2 | SQL | `server/pkg/db/queries/task_usage.sql` — add `ListDashboardRuntimeRunTime` |
| 3 | Go | `server/internal/handler/dashboard.go` — handlers; `router.go` — routes |
| 4 | TS core | `packages/core/types/agent.ts`, `api/schemas.ts`, `api/client.ts`, `dashboard/queries.ts` |
| 5 | TS views | `packages/views/dashboard/utils.ts` — util fns + unit tests |
| 6 | TS views | `packages/views/dashboard/components/dashboard-page.tsx` — Leaderboard scope |
| 7 | i18n | `packages/views/locales/{en,zh-Hans,ja,ko}/usage.json` |

## Key design decisions

1. **`—` zeroing** — time/tasks show `—` on model rows; tokens/cost show `—` on runtime rows. All 4 sort toggles remain visible in every scope.
2. **No new rollup table** — model scope reads `task_usage_hourly`; runtime scope reads `agent_task_queue` directly.
3. **Runtime names resolved client-side** from `runtimeListOptions` (fired via `DashboardPage`).

## New API routes

- `GET /api/dashboard/usage/by-model` — per-model token aggregates
- `GET /api/dashboard/runtime-runtime` — per-runtime run time + task counts
