# Time Tracking — Proposal

## Problem

Multica has a basic manual worklog system (introduced in `issue-worklog`) that lets members and agents record time spent on issues after the fact. There is no mechanism for **live time tracking**: starting a timer, watching it count up in real time, and stopping it to capture a completed time entry.

This gap means users who want to track time as they work must rely on an external tool (e.g. Toggl), then manually enter the result. For teams trying to understand actual time spent per issue or project — including time logged by AI agents — this creates friction and leads to inaccurate records.

The `worklog.type` field already reserves `'pomodoro'` for a timer-driven source. The `worklog_issue` join table already decouples time records from the issue entity. The proposed change builds on this foundation.

## Proposed Solution

Add a full **time tracking** feature to Multica:

1. **Backend**: A new `time_entry` table stores timer-driven time records (start time, stop time, duration). A `running_timer` lookup table enables O(1) current-timer queries per user. REST endpoints and WebSocket events mirror the established issue/comment/worklog API pattern.

2. **Frontend**: A global timer widget in the app shell header shows the running timer and allows start/stop. Issue detail pages gain a "Time" tab showing linked time entries. A standalone "My Time" page shows the user's personal time entry history. Timer entries can be linked to any issue.

## Core Concepts

### time_entry

A `time_entry` is the canonical live-timer record:

- Created when a user (or agent) starts a timer.
- Has a `start_time` and an optional `stop_time` (null = running).
- `duration_seconds` is negative while running (Toggl convention: `-start.Unix()`), positive when stopped.
- Can optionally be linked to an issue via `issue_id` (nullable).
- Can be created manually (with explicit start + stop) or via the timer UI.

### running_timer

A `running_timer` row exists for each user who currently has a live timer. It is a materialised cache pointing at the active `time_entry` for O(1) lookup. It is UPSERT-on-start, DELETE-on-stop.

### Relationship to existing worklog

The existing worklog feature (manual time logging on issues) remains unchanged. Time entries are a **parallel** and **complementary** model:

| Feature | Model | Source | Use case |
|---------|-------|---------|----------|
| Manual log | `worklog` | Human/agent manual input | "I spent 2 hours on this" |
| Live timer | `time_entry` | Timer start/stop | Tracks actual time as work happens |

Future work can aggregate both into unified reporting. There is no automatic conversion between the two in this change.

## Scope

**In scope:**
- `time_entry` + `running_timer` DB schema and migrations
- REST API: start timer, stop timer, list entries, get current entry, create manual entry, update entry, delete entry
- WebSocket events: `time_entry:started`, `time_entry:stopped`, `time_entry:updated`, `time_entry:deleted`
- Global timer widget in workspace app shell (shows elapsed time, stop button, issue link)
- Issue detail "Time" tab: list of time entries linked to this issue, total time summary
- "My Time" page: personal history of all time entries for the current user
- Timer can be linked to an issue, or free-form (no issue)
- Only one active timer per user at a time (starting a new one auto-stops the previous)

**Out of scope (future):**
- Pomodoro session mode with work/break intervals
- Time tracking for agents (agents use worklog; timer is user-driven)
- Team-wide time reports and analytics
- Calendar/timesheet views
- Billable tracking and client invoicing
- Bulk edit of time entries
- Drag-to-resize calendar blocks

## Open Questions

1. Should stopping a timer automatically offer to create a `worklog` entry on the linked issue? (Keeps the two models in sync but adds complexity.)
2. Should the "My Time" page be a top-level nav item or nested under a settings/profile area?
3. Should manual time entries on the timer page be supported in addition to the existing worklog panel in issue detail?
