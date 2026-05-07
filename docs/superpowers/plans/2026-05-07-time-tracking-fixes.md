# Time Tracking Bug Fixes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Fix 5 identified bugs in the time tracking feature (2 P0 correctness, 3 P1 UX).

**Architecture:** All 5 fixes are in the frontend (`apps/workspace/src/features/time-tracking/`). No backend schema changes. No new files — only targeted edits to existing files.

**Tech Stack:** TypeScript, React, Zustand, TanStack Query, Vitest

---

## File Map

| File | Change |
|---|---|
| `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts` | Fix ①: guard `time_entry:started` handler — only set currentEntry when entry is running |
| `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx` | Fix ②: deduplicate running entry in day list; Fix ⑤: use local date for grouping |
| `apps/workspace/src/features/time-tracking/components/TimeEntryEditSheet.tsx` | Fix ③: allow saving description/issue on running entries |
| `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx` | Fix ④: refresh running timer end every 30s |
| `apps/workspace/src/features/time-tracking/components/LiveDuration.test.tsx` | Existing test — verify still passes after changes |

---

## Task 1: Fix ① — WS sync incorrectly sets completed manual entry as currentEntry

**Problem:** When a manual entry (with `stop_time`) is created, the backend broadcasts `time_entry:started`. The WS sync handler blindly calls `setCurrentEntry(entry)`, putting a completed entry in the running-timer slot.

**Fix:** Add a guard — only set as `currentEntry` when `entry.duration_seconds < 0` (Toggl convention for running timer).

**Files:**
- Modify: `apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts`

- [ ] **Step 1: Open the file and locate the `time_entry:started` handler**

The handler is at line 31 in `use-time-tracking-sync.ts`:
```typescript
const unsubStarted = ws.on("time_entry:started", (raw) => {
  const { time_entry: entry } = raw as TimeEntryStartedPayload;
  const w = wid();
  queryClient.setQueryData(queryKeys.timeTracking.current(w), entry);
  useTimeTrackingStore.getState().setCurrentEntry(entry);
  void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
  if (entry.issue_id) {
    void queryClient.invalidateQueries({
      queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
    });
  }
});
```

- [ ] **Step 2: Add the guard for running-entry check**

Replace the handler body with:
```typescript
const unsubStarted = ws.on("time_entry:started", (raw) => {
  const { time_entry: entry } = raw as TimeEntryStartedPayload;
  const w = wid();
  // Only update the current-timer slot when the entry is actually running.
  // Manual entries (stop_time set, duration_seconds > 0) should not become currentEntry.
  if (entry.duration_seconds < 0) {
    queryClient.setQueryData(queryKeys.timeTracking.current(w), entry);
    useTimeTrackingStore.getState().setCurrentEntry(entry);
  }
  void queryClient.invalidateQueries({ queryKey: queryKeys.timeTracking.entries(w) });
  if (entry.issue_id) {
    void queryClient.invalidateQueries({
      queryKey: queryKeys.timeTracking.issueEntries(entry.issue_id),
    });
  }
});
```

- [ ] **Step 3: Run TypeScript type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add apps/workspace/src/features/time-tracking/hooks/use-time-tracking-sync.ts
git commit -m "fix(workspace): guard WS time_entry:started to only set currentEntry for running timers

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 2: Fix ② — MyTimePage shows running entry twice (in RunningTimerCard and day group)

**Problem:** `useTimeEntriesQuery` returns the running entry in the month list. `RunningTimerCard` also shows it. The running entry appears twice.

**Fix:** After fetching entries, filter out `currentEntry.id` from the list passed to `groupByDay`.

**Files:**
- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`

- [ ] **Step 1: Locate the entries and grouped memo in MyTimePage**

Around line 201-209 in `MyTimePage.tsx`:
```typescript
// API returns TimeEntry[] directly
const entries: TimeEntry[] = listData ?? [];

// Sort entries newest-first and group by day.
const grouped = useMemo(() => {
  const sorted = [...entries].sort(
    (a, b) => new Date(b.start_time).getTime() - new Date(a.start_time).getTime(),
  );
  return groupByDay(sorted);
}, [entries]);
```

- [ ] **Step 2: Filter out the running entry before grouping**

Replace the entries + grouped block with:
```typescript
// API returns TimeEntry[] directly
const entries: TimeEntry[] = listData ?? [];

// Exclude the running entry from the list view — it is already shown in RunningTimerCard above.
const listEntries = currentEntry
  ? entries.filter((e) => e.id !== currentEntry.id)
  : entries;

// Sort entries newest-first and group by day.
const grouped = useMemo(() => {
  const sorted = [...listEntries].sort(
    (a, b) => new Date(b.start_time).getTime() - new Date(a.start_time).getTime(),
  );
  return groupByDay(sorted);
}, [listEntries]);
```

- [ ] **Step 3: Run TypeScript type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx
git commit -m "fix(workspace): exclude running entry from MyTimePage day group to prevent duplication

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 3: Fix ③ — TimeEntryEditSheet blocks saving description/issue on running entries

**Problem:** Save button is `disabled={updateMutation.isPending || isRunning}`. For running entries, users can't update description or issue_id, even though the API supports it.

**Fix:** Allow saving when only description/issue changed. Only block saving start_time/stop_time on running entries (which are already hidden in the form anyway). In practice: remove `|| isRunning` from the Save button's disabled condition.

**Files:**
- Modify: `apps/workspace/src/features/time-tracking/components/TimeEntryEditSheet.tsx`

- [ ] **Step 1: Locate the Save button in TimeEntryEditSheet**

Around line 327-334:
```typescript
<Button
  size="sm"
  disabled={updateMutation.isPending || isRunning}
  onClick={handleSave}
>
  Save
</Button>
```

- [ ] **Step 2: Remove the `isRunning` disabled condition**

Replace with:
```typescript
<Button
  size="sm"
  disabled={updateMutation.isPending}
  onClick={handleSave}
>
  Save
</Button>
```

- [ ] **Step 3: Verify handleSave is safe for running entries**

`handleSave` (line 192-218) already skips `stop_time` validation when `isRunning` is true, and passes `stop_time: stopIso ?? undefined` — for running entries, `stopIso` is null (stop time field is hidden), so no stop_time is sent. The API's `UpdateTimeEntry` handler ignores nil stop_time. This is safe.

- [ ] **Step 4: Run TypeScript type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add apps/workspace/src/features/time-tracking/components/TimeEntryEditSheet.tsx
git commit -m "fix(workspace): allow editing description and issue on running timer in edit sheet

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 4: Fix ④ — Calendar timer event block doesn't extend as time passes

**Problem:** In `MyTimeCalendarPage`, the `events` useMemo uses `new Date()` at render time for running entry end. The calendar block stays frozen at the time of the last render.

**Fix:** Add a `now` state that updates every 30 seconds when a timer is running. Pass it into the `events` memo as a dependency.

**Files:**
- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx`

- [ ] **Step 1: Add `now` state with 30s interval**

Import `useEffect` and `useState` are already imported. Add the `now` state right after the `const [zoom, setZoom]` line (around line 164):

```typescript
// Tracks the current time so the running timer event block updates periodically.
const [now, setNow] = useState(() => new Date());
useEffect(() => {
  if (!running) return;
  const id = setInterval(() => setNow(new Date()), 30_000);
  return () => clearInterval(id);
}, [running]);
```

- [ ] **Step 2: Use `now` in the events memo instead of `new Date()`**

Locate the `events` memo (around line 200-218):
```typescript
const events = useMemo<CalendarEvent[]>(() => {
  const result: CalendarEvent[] = [];
  for (const entry of allEntries) {
    const start = new Date(entry.start_time);
    const end = entry.stop_time ? new Date(entry.stop_time) : new Date();
    ...
  }
  return result;
}, [allEntries]);
```

Replace with:
```typescript
const events = useMemo<CalendarEvent[]>(() => {
  const result: CalendarEvent[] = [];
  for (const entry of allEntries) {
    const start = new Date(entry.start_time);
    // Use the reactive `now` for running entries so the block extends as time passes.
    const end = entry.stop_time ? new Date(entry.stop_time) : now;
    const displayEnd = displayEndForCalendar(start, end);
    const segments = splitAtMidnight(start, displayEnd);
    for (const seg of segments) {
      result.push({
        id: entry.id,
        title: entry.description ?? "Time entry",
        start: seg.start,
        end: seg.end,
        resource: entry,
      });
    }
  }
  return result;
}, [allEntries, now]);
```

- [ ] **Step 3: Run TypeScript type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: no new errors.

- [ ] **Step 4: Commit**

```bash
git add apps/workspace/src/features/time-tracking/pages/MyTimeCalendarPage.tsx
git commit -m "fix(workspace): update calendar running timer block every 30s as time passes

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 5: Fix ⑤ — Timezone: groupByDay and formatDayLabel use UTC instead of local date

**Problem:** `groupByDay` slices `entry.start_time.slice(0, 10)` to get a UTC date. `formatDayLabel` compares `new Date().toISOString().slice(0, 10)` (also UTC). For users in UTC-N timezones, at 11 PM local time the UTC date is already tomorrow — entries logged at 11 PM local time group under "tomorrow" and show wrong "Today"/"Yesterday" labels.

**Fix:** Change both functions to derive the group key using local year/month/day.

**Files:**
- Modify: `apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx`

- [ ] **Step 1: Replace `groupByDay` with local-date-based version**

Current (line 24-33):
```typescript
function groupByDay(entries: TimeEntry[]): Map<string, TimeEntry[]> {
  const map = new Map<string, TimeEntry[]>();
  for (const entry of entries) {
    const key = entry.start_time.slice(0, 10);
    const bucket = map.get(key) ?? [];
    bucket.push(entry);
    map.set(key, bucket);
  }
  return map;
}
```

Replace with:
```typescript
/** Returns "YYYY-MM-DD" in local time for the given ISO timestamp. */
function localDateKey(isoString: string): string {
  const d = new Date(isoString);
  const y = d.getFullYear();
  const m = String(d.getMonth() + 1).padStart(2, "0");
  const day = String(d.getDate()).padStart(2, "0");
  return `${y}-${m}-${day}`;
}

/** Groups time entries by local calendar date (YYYY-MM-DD). */
function groupByDay(entries: TimeEntry[]): Map<string, TimeEntry[]> {
  const map = new Map<string, TimeEntry[]>();
  for (const entry of entries) {
    const key = localDateKey(entry.start_time);
    const bucket = map.get(key) ?? [];
    bucket.push(entry);
    map.set(key, bucket);
  }
  return map;
}
```

- [ ] **Step 2: Replace `formatDayLabel` with local-date-based version**

Current (line 35-42):
```typescript
function formatDayLabel(dateStr: string): string {
  const today = new Date().toISOString().slice(0, 10);
  const yesterday = new Date(Date.now() - 86400000).toISOString().slice(0, 10);
  if (dateStr === today) return "Today";
  if (dateStr === yesterday) return "Yesterday";
  return new Date(dateStr).toLocaleDateString(undefined, { month: "short", day: "numeric" });
}
```

Replace with:
```typescript
/** Formats a local "YYYY-MM-DD" key as "Today", "Yesterday", or "Jun 10". */
function formatDayLabel(dateStr: string): string {
  const today = localDateKey(new Date().toISOString());
  const yesterday = localDateKey(new Date(Date.now() - 86_400_000).toISOString());
  if (dateStr === today) return "Today";
  if (dateStr === yesterday) return "Yesterday";
  // Parse as local noon to avoid DST-edge midnight ambiguity.
  return new Date(`${dateStr}T12:00:00`).toLocaleDateString(undefined, {
    month: "short",
    day: "numeric",
  });
}
```

- [ ] **Step 3: Run unit tests**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm test
```
Expected: all tests pass.

- [ ] **Step 4: Run TypeScript type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: no new errors.

- [ ] **Step 5: Commit**

```bash
git add apps/workspace/src/features/time-tracking/pages/MyTimePage.tsx
git commit -m "fix(workspace): use local date for time entry day grouping and Today/Yesterday labels

Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

---

## Task 6: Final verification

- [ ] **Step 1: Run full type check**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm typecheck
```
Expected: 0 errors.

- [ ] **Step 2: Run all frontend unit tests**

```bash
cd /Users/chenjiaming/Developer/code/github/multica && pnpm test
```
Expected: all tests pass.

- [ ] **Step 3: Manual smoke test checklist**

1. Start a live timer → sidebar shows ticking counter ✓
2. Create a manual entry (with start+stop) → sidebar does NOT change to "running" state ✓
3. Open My Time page → running timer shows in RunningTimerCard only (not duplicated in day list) ✓
4. Open running entry in edit sheet → can type description and Save (not disabled) ✓
5. Open calendar view → running entry block grows over 30s ✓
6. (UTC-N timezone) entries at 11 PM local group under correct "Today" label ✓
