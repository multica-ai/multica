# M5 — Team & Project Time Review

## Problem

Time entries currently serve only the individual user. There is no way to answer:
- "How is the team spending its time this week?"
- "How much time has been invested in project X?"

## Scope

V1. No charts. Text tables only. Two surfaces:

1. **Project detail panel** — one "Time spent: X hours total" line
2. **`/team-time` route** — workspace-level breakdown by member and by project, with week/month filter

## Data Model

`time_entry` has `workspace_id`, `user_id`, `issue_id` (nullable). No `project_id` column.  
Project time is derived via `time_entry → issue.project_id`.  
Only stopped entries (`stop_time IS NOT NULL`) are counted in aggregates.

## Backend Changes

### New SQL queries (`server/pkg/db/queries/time_entry.sql`)

| Query | Returns |
|---|---|
| `SumTimeEntriesByUserInWorkspace` | `(user_id, total_seconds)` per member for workspace + date range |
| `SumTimeEntriesByProjectInWorkspace` | `(project_id, total_seconds)` per project (via JOIN issue) for workspace + date range |
| `SumTimeEntriesForProject` | `total_seconds` for a single project across all time |

### New endpoint

`GET /api/time-entries/team-stats?since=RFC3339&until=RFC3339`  
Response: `{ since, until, by_user: [{user_id, total_seconds}], by_project: [{project_id|null, total_seconds}] }`  
Authorization: workspace member.

`GET /api/projects/:id/time-stats`  
Response: `{ total_seconds: number }`  
Authorization: existing project membership check.

## Frontend Changes

### New types (`shared/types/time-entry.ts`)

```ts
TeamTimeUserStat, TeamTimeProjectStat, TeamTimeStats
```

### New page `features/time-tracking/pages/TeamTimePage.tsx`

- Header: "Team Time"
- Time range selector: This Week / This Month / Last Month (buttons)
- Section: "By Member" — table: member name + formatted hours (descending)
- Section: "By Project" — table: project name + formatted hours + unlinked row

### Router / Nav

- Route `/team-time` added to router
- "Team Time" added to `workspaceNav` in `navigation.ts`

### Project detail

`projects-page.tsx` → `ProjectDetailPanel`:  
Add one stat row between the progress bar and the lead row:  
`Time spent: X h Y m total` (from `GET /api/projects/:id/time-stats`)
