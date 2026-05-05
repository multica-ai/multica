# Time Tracking — Design

## Database Schema

### New table: `time_entry`

```sql
CREATE TABLE time_entry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    description TEXT,
    start_time TIMESTAMPTZ NOT NULL,
    stop_time TIMESTAMPTZ,
    -- Negative while running (= -start_time.Unix()), positive when stopped (seconds).
    duration_seconds INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_time_entry_workspace_user ON time_entry (workspace_id, user_id, start_time DESC);
CREATE INDEX idx_time_entry_issue ON time_entry (issue_id) WHERE issue_id IS NOT NULL;
```

**Running convention (from Toggl):**
- While a timer is running: `stop_time = NULL`, `duration_seconds = -start_time.Unix()` (a large negative number)
- After stopping: `stop_time = <timestamp>`, `duration_seconds = (stop_time - start_time).seconds` (positive)

This convention lets the frontend compute elapsed time as `now.Unix() + duration_seconds` when `duration_seconds < 0`, without depending on the server clock.

### New table: `running_timer`

```sql
CREATE TABLE running_timer (
    user_id UUID PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE,
    time_entry_id UUID NOT NULL REFERENCES time_entry(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

A single row per user. UPSERT on timer start, DELETE on timer stop or timer delete. Acts as a materialised index for O(1) lookups — avoids full scans of `time_entry` to find the running row.

---

## API Design

All endpoints require JWT auth. `workspace_id` is provided via the `X-Workspace-ID` header or path parameter.

### Timer lifecycle

| Method | Path | Description |
|--------|------|-------------|
| `POST` | `/workspaces/:workspace_id/time-entries` | Start timer or create manual entry |
| `PATCH` | `/workspaces/:workspace_id/time-entries/:id/stop` | Stop running timer |
| `GET` | `/workspaces/:workspace_id/time-entries/current` | Get current running timer for authenticated user |
| `GET` | `/workspaces/:workspace_id/time-entries` | List time entries for the current user (paginated, most recent first) |
| `GET` | `/workspaces/:workspace_id/issues/:id/time-entries` | List time entries linked to an issue |
| `PATCH` | `/workspaces/:workspace_id/time-entries/:id` | Update description or issue link of a time entry |
| `DELETE` | `/workspaces/:workspace_id/time-entries/:id` | Delete a time entry |

### Request/Response shapes

**POST /workspaces/:workspace_id/time-entries** — Start or create:
```json
{
  "description": "Working on auth flow",
  "issue_id": "uuid-or-null",
  "start_time": "2026-01-01T10:00:00Z",
  "stop_time": "2026-01-01T11:30:00Z"  // omit to start live timer
}
```

If `stop_time` is omitted, a live timer is started. If the user already has a running timer, it is automatically stopped first.

**Response** (shared shape for all entry endpoints):
```json
{
  "id": "uuid",
  "workspace_id": "uuid",
  "user_id": "uuid",
  "issue_id": "uuid-or-null",
  "description": "Working on auth flow",
  "start_time": "2026-01-01T10:00:00Z",
  "stop_time": null,
  "duration_seconds": -1735725600,
  "created_at": "...",
  "updated_at": "..."
}
```

**PATCH /workspaces/:workspace_id/time-entries/:id/stop** — Stop running timer:
- No request body needed; backend sets `stop_time = now()` and `duration_seconds = positive`.

**PATCH /workspaces/:workspace_id/time-entries/:id** — Update entry:
```json
{
  "description": "Updated description",
  "issue_id": "new-uuid-or-null"
}
```

### One active timer rule

`CreateTimeEntry` (the service layer) auto-stops any existing running timer before creating the new one. This mirrors the Toggl `CreateTimeEntry` contract documented in the reference material.

### WebSocket Events

| Event | Payload |
|-------|---------|
| `time_entry:started` | `{ time_entry: TimeEntryResponse }` |
| `time_entry:stopped` | `{ time_entry: TimeEntryResponse }` |
| `time_entry:updated` | `{ time_entry: TimeEntryResponse }` |
| `time_entry:deleted` | `{ time_entry_id: string }` |

Events are broadcast to all clients in the same workspace, scoped by `workspace_id`.

---

## Backend Architecture

```
server/
├── internal/handler/
│   └── time_entry.go         # HTTP handler (CRUD + start/stop)
├── internal/service/
│   └── time_entry.go         # Business logic (auto-stop, duration calc)
├── pkg/db/queries/
│   └── time_entry.sql        # sqlc SQL queries
├── migrations/
│   ├── 036_time_entry.up.sql
│   └── 036_time_entry.down.sql
```

**Service layer responsibilities (`service/time_entry.go`):**
- `StartTimer(workspaceID, userID, description, issueID)` → auto-stops existing, creates new entry + running_timer row
- `StopTimer(workspaceID, userID, timeEntryID)` → sets stop_time, computes duration, removes running_timer row
- `GetCurrentTimer(workspaceID, userID)` → fast lookup via running_timer, falls back to full scan + repair if needed
- `ListTimeEntries(workspaceID, userID, limit, offset)` → paginated list
- `ListIssueTimeEntries(workspaceID, issueID)` → entries linked to issue
- `UpdateTimeEntry(workspaceID, userID, timeEntryID, updates)` → update description/issue link
- `DeleteTimeEntry(workspaceID, userID, timeEntryID)` → delete + clear running_timer if running

**Handler routes (to be wired in `cmd/server/router.go`):**
```
POST   /workspaces/{id}/time-entries
GET    /workspaces/{id}/time-entries
GET    /workspaces/{id}/time-entries/current
PATCH  /workspaces/{id}/time-entries/{entry_id}/stop
PATCH  /workspaces/{id}/time-entries/{entry_id}
DELETE /workspaces/{id}/time-entries/{entry_id}
GET    /workspaces/{id}/issues/{issue_id}/time-entries
```

---

## Frontend Architecture

```
apps/workspace/src/
├── features/
│   └── time-tracking/
│       ├── index.ts                    # Public exports
│       ├── store.ts                    # Zustand store (current timer state)
│       ├── api.ts                      # API functions (REST calls)
│       ├── hooks/
│       │   ├── useTimeEntries.ts       # TanStack Query hooks
│       │   ├── useCurrentTimer.ts      # Current running timer (polls 30s)
│       │   └── useTimerMutations.ts    # Start/stop/update/delete mutations
│       ├── components/
│       │   ├── GlobalTimerWidget.tsx   # App shell header widget
│       │   ├── TimerButton.tsx         # Start/stop button
│       │   ├── LiveDuration.tsx        # Elapsed time counter (1s interval)
│       │   ├── IssueTimerSection.tsx   # Time tab inside issue detail
│       │   ├── TimeEntryList.tsx       # List of entries
│       │   ├── TimeEntryRow.tsx        # Single entry row
│       │   └── TimeEntryForm.tsx       # Create/edit form
│       └── utils.ts                    # Duration formatting, time helpers
└── shared/
    └── types/
        └── time-entry.ts               # TimeEntry type definition
```

### State Management

**Zustand store (`features/time-tracking/store.ts`):**
```typescript
interface TimeTrackingState {
  // The current running timer (null if no active timer)
  currentEntry: TimeEntry | null;
  setCurrentEntry: (entry: TimeEntry | null) => void;
}
```

Single store per feature. The `currentEntry` is shared by `GlobalTimerWidget` and `IssueTimerSection` so they stay in sync.

### TanStack Query hooks

**`useCurrentTimer`:**
- `queryKey: ['time-tracking', 'current', workspaceId]`
- `refetchInterval: 30_000` (30s polling)
- `refetchOnWindowFocus: true`
- Returns `TimeEntry | null`

**`useTimeEntries`:**
- `queryKey: ['time-tracking', 'entries', workspaceId]`
- Paginated, most recent first

**`useIssueTimeEntries`:**
- `queryKey: ['time-tracking', 'issue', issueId]`

### Optimistic Updates

**Start timer (`onMutate`):**
1. Construct a temporary entry with `id: 'optimistic-' + Date.now()`, `start_time: now`, `duration_seconds: -now.Unix()`
2. `setQueryData` for current timer key → shows timer immediately without waiting for server

**Stop timer (`onMutate`):**
1. Set current timer cache to `null` → stops the live counter instantly
2. Update the entry in list caches with the computed stop time
3. `onError` → roll back to previous snapshot

### Realtime Sync

`useWSEvent` subscribes to `time_entry:started`, `time_entry:stopped`, `time_entry:updated`, `time_entry:deleted` events and calls `invalidateQueries` on the relevant keys. This ensures that if another tab or device starts/stops a timer, all open tabs update.

### LiveDuration component

```typescript
// Initialise from entry.start_time (not from 0) to avoid flash on page refresh
const [seconds, setSeconds] = useState(() => 
  Math.floor((Date.now() - new Date(entry.start_time).getTime()) / 1000)
);

useEffect(() => {
  const id = setInterval(() => {
    setSeconds(Math.floor((Date.now() - new Date(entry.start_time).getTime()) / 1000));
  }, 1000);
  return () => clearInterval(id);
}, [entry.start_time]);
```

---

## UI Surfaces

### 1. Global Timer Widget (app shell header)

Located in the top navigation bar, always visible when logged in.

**States:**
- **Idle**: Shows a clock icon + "Track time" placeholder text. Click to expand an input popover.
- **Running**: Shows elapsed time (live counter), issue name (if linked), stop button (■). Click elapsed time to open the entry editor popover.

**Popover (idle):**
- Text input for description
- Issue picker (optional)
- Start button

### 2. Issue Detail — "Time" Tab

Inside the existing issue detail layout, adds a tab (alongside Comments, Activity, etc.).

**Contents:**
- Total time logged badge: `∑ X h Ym` (sum of all completed time entries linked to this issue)
- "Start tracking this issue" button → starts a timer with `issue_id` pre-filled
- List of time entries: author avatar, description, duration, start date, delete button
- Empty state when no entries

### 3. My Time Page

Route: `/my-time`

A personal time tracking history page.

**Layout:**
- Top: running timer summary card (if active) with stop button
- Below: paginated list of time entries grouped by day
- Each entry row: time range, duration, description, linked issue (if any), edit/delete actions

---

## Routing

New route added to the workspace app:

```typescript
// apps/workspace/src/router.tsx
const myTimeRoute = createRoute({
  getParentRoute: () => appShellRoute,
  path: "/my-time",
  component: MyTimePage,
});
```

---

## Data Flow Diagram

```
User clicks "Start" in GlobalTimerWidget
  → useStartTimerMutation.mutateAsync({ description, issueId })
    → onMutate: setQueryData(currentTimerKey, optimisticEntry)
    → POST /workspaces/:id/time-entries
      → handler.StartTimer()
        → service.StartTimer()
          → auto-stop existing running_timer (if any)
          → INSERT time_entry (stop_time=NULL, duration_seconds<0)
          → UPSERT running_timer (user_id, time_entry_id)
          → broadcast WS event: time_entry:started
    → onSuccess: invalidateQueries([currentTimerKey])
    → onError: rollback to previous state

LiveDuration component (1s interval):
  elapsed = (Date.now() - entry.start_time) / 1000

User clicks "Stop" in GlobalTimerWidget
  → useStopTimerMutation.mutateAsync({ timeEntryId })
    → onMutate: setQueryData(currentTimerKey, null)
    → PATCH /workspaces/:id/time-entries/:id/stop
      → service.StopTimer()
        → UPDATE time_entry (stop_time=now(), duration_seconds=positive)
        → DELETE running_timer WHERE user_id=$1
        → broadcast WS event: time_entry:stopped
    → onSuccess: invalidateQueries
```

---

## Migration Plan

| # | Migration | Description |
|---|-----------|-------------|
| 036 | `036_time_entry.up.sql` | Create `time_entry` and `running_timer` tables + indexes |

**Up migration:**
```sql
CREATE TABLE time_entry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workspace_id UUID NOT NULL REFERENCES workspace(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES "user"(id) ON DELETE CASCADE,
    issue_id UUID REFERENCES issue(id) ON DELETE SET NULL,
    description TEXT,
    start_time TIMESTAMPTZ NOT NULL,
    stop_time TIMESTAMPTZ,
    duration_seconds INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_time_entry_workspace_user ON time_entry (workspace_id, user_id, start_time DESC);
CREATE INDEX idx_time_entry_issue ON time_entry (issue_id) WHERE issue_id IS NOT NULL;

CREATE TABLE running_timer (
    user_id UUID PRIMARY KEY REFERENCES "user"(id) ON DELETE CASCADE,
    time_entry_id UUID NOT NULL REFERENCES time_entry(id) ON DELETE CASCADE,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

**Down migration:**
```sql
DROP TABLE IF EXISTS running_timer;
DROP TABLE IF EXISTS time_entry;
```

---

## Decisions

### D1: Separate `time_entry` table (not extending `worklog`)

The existing `worklog` table requires a complete `duration_minutes` at insert time and has no concept of a "running" state. Adding a nullable duration and a start_time would require a schema change and break the existing worklog API contract. A separate `time_entry` table is cleaner and maps naturally to the live-timer model.

### D2: Negative duration convention

Adopted from Toggl's well-tested design: `duration_seconds = -start_time.Unix()` while running. This lets the frontend compute elapsed time purely from the `start_time` field without depending on server clock drift, and avoids storing a separate "is_running" boolean that can get out of sync.

### D3: `running_timer` as a materialised index

A full scan of `time_entry` to find the running entry is O(n). With a `running_timer` table, the lookup is O(1). The fallback (scan + repair) handles entries created through import or edge cases, matching the Toggl reference design.

### D4: Auto-stop on new timer start

One active timer per user enforced at the service layer. Starting a new timer auto-stops the previous one. This simplifies the UI (no need to juggle multiple timers) and matches user expectations from Toggl-style tools.

### D5: Timer is user-driven only

Agents use the existing `worklog` model for logging time. Timers require a real-time interaction model (start/stop) that does not map naturally to autonomous agent execution. Future work could add agent-driven `time_entry` creation if needed.

### D6: `issue_id` is nullable on `time_entry`

Users often want to track time before they know which issue to assign it to, or for general work not tied to any issue. The issue link can be set or updated after the fact via PATCH.
