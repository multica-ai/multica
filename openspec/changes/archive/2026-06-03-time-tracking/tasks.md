# Time Tracking — Tasks

## Phase 1: Database & Backend Foundation

### T01 — DB migration: time_entry + running_timer tables
- Create `server/migrations/036_time_entry.up.sql` with `time_entry` and `running_timer` table definitions and indexes
- Create `server/migrations/036_time_entry.down.sql`

### T02 — SQL queries: time_entry CRUD
- Add `server/pkg/db/queries/time_entry.sql` with queries:
  - `CreateTimeEntry`
  - `GetTimeEntryByID`
  - `ListTimeEntriesByUser`
  - `ListTimeEntriesByIssue`
  - `UpdateTimeEntry` (description, issue_id, stop_time, duration_seconds)
  - `DeleteTimeEntry`
- Run `make sqlc` to regenerate `pkg/db/generated/`

### T03 — SQL queries: running_timer operations
- Add queries to `time_entry.sql`:
  - `SetRunningTimer` (UPSERT)
  - `GetRunningTimerByUser` (JOIN with time_entry)
  - `ClearRunningTimerByUser` (DELETE)
- Run `make sqlc`

### T04 — Service: time entry business logic
- Create `server/internal/service/time_entry.go`
- Implement:
  - `StartTimer(workspaceID, userID, description, issueID) → TimeEntry`
  - `StopTimer(workspaceID, userID, timeEntryID) → TimeEntry`
  - `GetCurrentTimer(workspaceID, userID) → *TimeEntry`
  - `ListTimeEntries(workspaceID, userID, limit, offset) → []TimeEntry`
  - `ListIssueTimeEntries(workspaceID, issueID) → []TimeEntry`
  - `UpdateTimeEntry(workspaceID, userID, timeEntryID, updates) → TimeEntry`
  - `DeleteTimeEntry(workspaceID, userID, timeEntryID)`
- One-active-timer rule: auto-stop before starting new

### T05 — Handler: HTTP endpoints
- Create `server/internal/handler/time_entry.go`
- Implement handlers for all 7 routes
- Wire up in `cmd/server/router.go`
- Broadcast WebSocket events: `time_entry:started`, `time_entry:stopped`, `time_entry:updated`, `time_entry:deleted`

### T06 — Backend tests
- Handler and service tests in `time_entry_test.go`
- Test: start timer, stop timer, auto-stop on second start, current timer query, delete running timer, list entries

---

## Phase 2: Frontend Foundation

### T07 — TypeScript type: TimeEntry
- Add `apps/workspace/src/shared/types/time-entry.ts`
- Export `TimeEntry` interface matching API response shape

### T08 — API functions
- Create `apps/workspace/src/features/time-tracking/api.ts`
- Wrap all 7 REST endpoints with typed fetch helpers

### T09 — Zustand store
- Create `apps/workspace/src/features/time-tracking/store.ts`
- State: `currentEntry: TimeEntry | null`
- Actions: `setCurrentEntry`

### T10 — TanStack Query hooks
- Create `apps/workspace/src/features/time-tracking/hooks/`
- `useCurrentTimer`: polls every 30s, `refetchOnWindowFocus: true`
- `useTimeEntries`: paginated list
- `useIssueTimeEntries`: entries for a specific issue
- `useStartTimerMutation`: optimistic update (creates temporary entry in cache)
- `useStopTimerMutation`: optimistic update (clears current entry from cache)
- `useUpdateTimeEntryMutation`
- `useDeleteTimeEntryMutation`

### T11 — LiveDuration component
- Create `apps/workspace/src/features/time-tracking/components/LiveDuration.tsx`
- Initialises from `entry.start_time` (no flash from 0)
- 1-second interval ticker
- Formats as `h:mm:ss`

### T12 — Realtime sync
- Subscribe to WS events in time-tracking feature
- On `time_entry:started/stopped/updated/deleted`: `invalidateQueries` on affected keys
- Register event names in `apps/workspace/src/shared/types/`

---

## Phase 3: Global Timer Widget

### T13 — TimerButton + GlobalTimerWidget component
- `GlobalTimerWidget.tsx`: renders idle state and running state
- Idle state: clock icon, "Track time" text, click opens start popover
- Running state: `<LiveDuration>`, issue name, stop button
- Start popover: description input, issue picker, start button

### T14 — Wire GlobalTimerWidget into app shell
- Add `GlobalTimerWidget` to the top navigation bar in `app-shell.tsx`

---

## Phase 4: Issue Detail Time Tab

### T15 — IssueTimerSection component
- Create `apps/workspace/src/features/time-tracking/components/IssueTimerSection.tsx`
- Shows total time badge
- "Start tracking" button (pre-fills `issue_id`)
- `TimeEntryList` and `TimeEntryRow` sub-components
- Empty state

### T16 — Add Time tab to issue detail
- Extend issue detail page to include a "Time" tab
- Render `IssueTimerSection` when tab is active

---

## Phase 5: My Time Page

### T17 — My Time page
- Create `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`
- Running timer summary card at top
- Day-grouped list of time entries
- Each row: time range, duration, description, linked issue, edit/delete

### T18 — Add /my-time route
- Add route in `apps/workspace/src/router.tsx`
- Add navigation link in sidebar

---

## Phase 6: Polish & Verification

### T19 — document.title timer
- When a timer is running, update `document.title` to show elapsed time (e.g. `1:23:45 · Multica`)
- Clear on stop

### T20 — Full verification
- Run `make check` (typecheck + unit tests + Go tests + E2E)
- Fix any failures
