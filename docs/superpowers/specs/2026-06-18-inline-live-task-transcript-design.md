# Inline Live Task Transcript in Execution Log

**Status:** Approved for implementation  
**Author:** Claude Code  
**Date:** 2026-06-18  
**Related context:** `server/pkg/agent/csc.go`, `packages/views/issues/components/execution-log-section.tsx`, `packages/views/common/task-transcript/agent-transcript-dialog.tsx`

## 1. Background

Multica's daemon executes agent tasks through multiple provider backends, including `csc`. The `csc` backend reuses `streamJSONBackend`, which parses the same line-delimited stream-json protocol as Claude Code and normalizes events into typed `agent.Message` values: `text`, `thinking`, `tool_use`, `tool_result`, `status`, `log`, and `error`.

These messages are already persisted in `multica_task_message`, exposed via `GET /api/tasks/{taskId}/messages`, and pushed to the frontend in real time through `task:message` WebSocket events. The frontend already renders them inside a modal transcript dialog (`AgentTranscriptDialog`).

This design moves that transcript out of a modal and embeds it inline in the **Execution Log** section of the issue detail page, so users can watch a csc run unfold without leaving the page.

## 2. Goal

Add an expandable, live-updating transcript panel directly inside each task row in the Execution Log section. The panel must:

- Show the parsed conversation timeline for any task (active or terminal).
- Update in real time for running tasks using the existing WebSocket pipeline.
- Reuse the same rendering, redaction, and event styling as the existing modal transcript.
- Not require any new backend endpoints or WS topics.

## 3. Non-goals

- Do not build a raw stream-json view; the panel shows parsed `TimelineItem` events only.
- Do not add new persistence for messages; rely on existing `multica_task_message` rows.
- Do not replace the modal transcript; keep `AgentTranscriptDialog` available for users who prefer a focused view.

## 4. Component architecture

```
packages/views/common/task-transcript/
├── agent-transcript-dialog.tsx   # refactored to use TaskTranscriptTimeline
├── transcript-button.tsx         # unchanged
├── build-timeline.ts             # unchanged
├── redact.ts                     # unchanged
└── task-transcript-timeline.tsx  # NEW: shared timeline renderer

packages/views/issues/components/execution-log/
├── execution-log-section.tsx     # existing; add InlineTranscriptPanel below each row
└── inline-transcript-panel.tsx   # NEW: wrapper with chrome + data fetching
```

### 4.1 `TaskTranscriptTimeline`

A pure presentational component extracted from the body of `AgentTranscriptDialog`.

**Props:**

```ts
interface TaskTranscriptTimelineProps {
  items: TimelineItem[];
  isLive?: boolean;
  className?: string;
  // Optional: if omitted, the inline view renders all items without filtering.
  filter?: { selectedTools: Set<string> };
}
```

**Responsibilities:**

- Render items in chronological order.
- Apply `redactSecrets` (caller passes already-redacted `TimelineItem[]` from `buildTimeline`).
- Show type-specific styling for `text`, `thinking`, `tool_use`, `tool_result`, `error`.
- Support auto-scroll-to-bottom when `isLive` is true and the user is already at the bottom.

### 4.2 `InlineTranscriptPanel`

A local wrapper rendered inside each `ActiveRow` and `PastRow`.

**Props:**

```ts
interface InlineTranscriptPanelProps {
  task: AgentTask;
  isLive?: boolean;
  defaultOpen?: boolean;
}
```

**Responsibilities:**

- Fetch messages with `useQuery(taskMessagesOptions(task.id))`.
- Convert `TaskMessagePayload[]` to `TimelineItem[]` with `buildTimeline`.
- Render an expand/collapse toggle.
- Render loading, empty, and error states.
- Provide a bounded scroll container.
- Forward `isLive` to `TaskTranscriptTimeline` for the live indicator and auto-scroll behavior.

## 5. Data flow

1. Daemon batches `agent.Message` events into `TaskMessageData` and posts them to `/api/daemon/tasks/{taskId}/messages`.
2. Server persists rows and publishes `task:message` WebSocket events.
3. `useRealtimeSync` receives the event and writes it into the TanStack Query cache key `["task-messages", task_id]`.
4. `InlineTranscriptPanel` subscribes to the same cache key via `useQuery(taskMessagesOptions(task.id))`, so new messages appear instantly.
5. The panel calls `buildTimeline(messages)` to derive `TimelineItem[]` and passes them to `TaskTranscriptTimeline`.

No polling, no new WS subscription, and no Zustand store are introduced.

## 6. UI/UX behavior

### 6.1 Placement

Each row in `ExecutionLogSection` gains a second visual tier below the trigger/status/actions line. The inline panel is rendered inside `RowShell` after the primary row content.

### 6.2 Default open state

- **Active rows:** expand automatically when at least one message exists (`task.status !== "queued"`). This matches the current rule that hides the transcript button for queued tasks.
- **Past rows:** collapsed by default; user must click "Show transcript".

### 6.3 Scroll behavior

- Panel height is capped (e.g., `max-h-80`).
- When `isLive` is true and the user is scrolled to the bottom, new items auto-scroll into view.
- If the user scrolls up to read earlier events, auto-scroll pauses until they return to the bottom.
- A subtle "New events" indicator appears when items arrived while scrolled up.

### 6.4 Live indicator

When `isLive && task.status === "running"`, the panel header shows a pulsing dot and the label "Live". The indicator stops once the task reaches a terminal state.

### 6.5 Minimal inline filters

The first version keeps filters simple:

- A single "Show thinking" toggle, defaulting to the user's last choice from `useTranscriptViewStore` if available.
- Full tool-type filtering and sort direction remain in the modal dialog only.

Future iterations can move the full filter bar into the inline panel.

## 7. Error handling & edge cases

| Scenario | Behavior |
|---|---|
| Task is `queued` | Panel is not rendered; toggle is hidden. |
| Fetch returns no messages | Render "No events yet." For live runs, the message updates automatically when the first event arrives. |
| Fetch fails | Render inline error with a retry button. Row actions remain usable. |
| Duplicate `task:message` events | `useRealtimeSync` already dedupes by `seq` before writing to the cache. |
| WS reconnect | `useWSReconnect` re-hydrates active tasks; the inline panel's query refetches automatically. |
| Very long tool output | `TaskMessageData` already truncates tool results to 8KB server-side; the timeline renders what it receives. |

## 8. Refactoring impact

- `AgentTranscriptDialog` loses its inline timeline rendering and imports `TaskTranscriptTimeline` instead. No public prop changes.
- `TranscriptButton` remains unchanged; users can still open the modal.
- `ExecutionLogSection` imports `InlineTranscriptPanel` and renders it below each row.

## 9. Testing plan

### 9.1 Unit / component tests

- `packages/views/execution-log-section.test.tsx`
  - Expanding an active row renders fetched messages as timeline items.
  - Simulating a `task:message` WebSocket event appends a new item without refetching.
  - Collapsed past rows do not fetch messages until expanded.

- `packages/views/task-transcript-timeline.test.tsx` (new)
  - Renders all five `TimelineItem` types with correct labels and colors.
  - Auto-scrolls to bottom on new items when already at bottom.
  - Pauses auto-scroll when user scrolls up.

- `packages/views/agent-transcript-dialog.test.tsx`
  - Modal still opens and renders the same content after the refactor.

### 9.2 E2E test

- Start a task assigned to a csc runtime.
- Open the issue detail page and expand the Execution Log.
- Expand the live transcript panel.
- Verify that streamed events (text, tool_use, tool_result) appear in real time without a page reload.

## 10. Open questions

None at design time. All decisions were confirmed during brainstorming:

- The backend already supports csc real-time messages; no new API work is required.
- The modal transcript is preserved; the inline panel is additive.
