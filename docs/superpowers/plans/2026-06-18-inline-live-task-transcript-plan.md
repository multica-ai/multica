# Inline Live Task Transcript Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:subagent-driven-development` (recommended) or `superpowers:executing-plans` to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Embed an expandable, live-updating task transcript panel directly inside each row of the issue Execution Log section, reusing the existing `task:message` WebSocket pipeline.

**Architecture:** Extract the event-row rendering from `AgentTranscriptDialog` into a shared `TaskTranscriptTimeline` component. Build a new `InlineTranscriptPanel` that fetches `taskMessagesOptions`, renders the timeline, and wires it into `ExecutionLogSection`. No backend changes.

**Tech Stack:** React, TypeScript, Tailwind CSS, TanStack Query, Vitest, React Testing Library, shadcn/ui Collapsible.

---

## File Structure

| File | Change | Responsibility |
|---|---|---|
| `packages/views/common/task-transcript/task-transcript-timeline.tsx` | Create | Shared event-row renderer extracted from the dialog body. |
| `packages/views/common/task-transcript/task-transcript-timeline.test.tsx` | Create | Tests rendering of all timeline item types. |
| `packages/views/common/task-transcript/agent-transcript-dialog.tsx` | Modify | Refactor to import `TaskTranscriptTimeline`; keep header, filters, timeline bar, metadata. |
| `packages/views/common/task-transcript/index.ts` | Modify | Export `TaskTranscriptTimeline`. |
| `packages/views/locales/en/issues.json` | Modify | Add inline transcript strings. |
| `packages/views/locales/zh-Hans/issues.json` | Modify | Add inline transcript Chinese strings. |
| `packages/views/issues/components/execution-log/inline-transcript-panel.tsx` | Create | Expandable panel with query, live indicator, auto-scroll, empty/error states. |
| `packages/views/issues/components/execution-log/inline-transcript-panel.test.tsx` | Create | Tests fetching, expanding, and live message updates. |
| `packages/views/issues/components/execution-log-section.tsx` | Modify | Render `InlineTranscriptPanel` below each active and past task row. |
| `packages/views/issues/components/execution-log-section.test.tsx` | Create | Tests that rows render the panel and it expands on click. |

---

## Task 1: Extract `TaskTranscriptTimeline`

**Files:**
- Create: `packages/views/common/task-transcript/task-transcript-timeline.tsx`
- Create: `packages/views/common/task-transcript/task-transcript-timeline.test.tsx`
- Modify: `packages/views/common/task-transcript/index.ts`

### Step 1: Write the failing test

Create `packages/views/common/task-transcript/task-transcript-timeline.test.tsx`:

```tsx
import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";
import { TaskTranscriptTimeline } from "./task-transcript-timeline";
import type { TimelineItem } from "./build-timeline";

const items: TimelineItem[] = [
  { seq: 1, type: "text", content: "Hello" },
  { seq: 2, type: "thinking", content: "Planning..." },
  { seq: 3, type: "tool_use", tool: "Bash", input: { command: "ls" } },
  { seq: 4, type: "tool_result", tool: "Bash", output: "file.txt" },
  { seq: 5, type: "error", content: "Something broke" },
];

describe("TaskTranscriptTimeline", () => {
  it("renders all event types", () => {
    render(<TaskTranscriptTimeline items={items} />);
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("Thinking")).toBeInTheDocument();
    expect(screen.getByText("Bash")).toBeInTheDocument();
    expect(screen.getByText("Error")).toBeInTheDocument();
  });

  it("renders empty state when no items", () => {
    render(<TaskTranscriptTimeline items={[]} />);
    expect(screen.getByText("No events yet.")).toBeInTheDocument();
  });

  it("renders live empty state when isLive and no items", () => {
    render(<TaskTranscriptTimeline items={[]} isLive />);
    expect(screen.getByText("Waiting for events...")).toBeInTheDocument();
  });
});
```

### Step 2: Run the test to verify it fails

Run:

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/task-transcript-timeline.test.tsx
```

Expected: FAIL with "Cannot find module './task-transcript-timeline'".

### Step 3: Implement `TaskTranscriptTimeline`

Create `packages/views/common/task-transcript/task-transcript-timeline.tsx`. Move the helpers `getEventColor`, `getEventLabel`, `getEventSummary`, `colorClasses`, `shortenPath`, the `TranscriptEventRow` component, and `EventDetailContent` from `agent-transcript-dialog.tsx`. Add the new exported component.

```tsx
"use client";

import { useState } from "react";
import { Brain, AlertCircle, ChevronRight } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Collapsible, CollapsibleContent, CollapsibleTrigger } from "@multica/ui/components/ui/collapsible";
import type { TimelineItem } from "./build-timeline";
import { redactSecrets } from "./redact";

export interface TaskTranscriptTimelineProps {
  items: TimelineItem[];
  isLive?: boolean;
  className?: string;
  emptyLabel?: string;
  liveEmptyLabel?: string;
}

type EventColor = "agent" | "thinking" | "tool" | "result" | "error";

const colorClasses: Record<EventColor, { bg: string; bgActive: string; label: string }> = {
  agent: { bg: "bg-emerald-400/60", bgActive: "bg-emerald-500", label: "bg-emerald-500" },
  thinking: { bg: "bg-violet-400/60", bgActive: "bg-violet-500", label: "bg-violet-500/20 text-violet-700 dark:text-violet-300" },
  tool: { bg: "bg-blue-400/60", bgActive: "bg-blue-500", label: "bg-blue-500/20 text-blue-700 dark:text-blue-300" },
  result: { bg: "bg-slate-300/60 dark:bg-slate-600/60", bgActive: "bg-slate-400 dark:bg-slate-500", label: "bg-muted text-muted-foreground" },
  error: { bg: "bg-red-400/60", bgActive: "bg-red-500", label: "bg-red-500/20 text-red-700 dark:text-red-300" },
};

function getEventColor(item: TimelineItem): EventColor {
  switch (item.type) {
    case "text": return "agent";
    case "thinking": return "thinking";
    case "tool_use": return "tool";
    case "tool_result": return "result";
    case "error": return "error";
    default: return "result";
  }
}

function getEventLabel(item: TimelineItem): string {
  switch (item.type) {
    case "text": return "Agent";
    case "thinking": return "Thinking";
    case "tool_use": return item.tool ?? "Tool";
    case "tool_result": return item.tool ? `${item.tool}` : "Result";
    case "error": return "Error";
    default: return "Event";
  }
}

function getEventSummary(item: TimelineItem): string {
  switch (item.type) {
    case "text":
      return item.content?.split("\n").find((l) => l.trim().length > 0) ?? "";
    case "thinking":
      return item.content?.slice(0, 200) ?? "";
    case "tool_use": {
      if (!item.input) return "";
      const inp = item.input as Record<string, string>;
      if (inp.query) return inp.query;
      if (inp.file_path) return shortenPath(inp.file_path);
      if (inp.path) return shortenPath(inp.path);
      if (inp.pattern) return inp.pattern;
      if (inp.description) return String(inp.description);
      if (inp.command) {
        const cmd = String(inp.command);
        return cmd.length > 120 ? cmd.slice(0, 120) + "..." : cmd;
      }
      if (inp.prompt) {
        const p = String(inp.prompt);
        return p.length > 120 ? p.slice(0, 120) + "..." : p;
      }
      if (inp.skill) return String(inp.skill);
      for (const v of Object.values(inp)) {
        if (typeof v === "string" && v.length > 0 && v.length < 120) return v;
      }
      return "";
    }
    case "tool_result":
      return item.output?.slice(0, 200) ?? "";
    case "error":
      return item.content ?? "";
    default:
      return "";
  }
}

function shortenPath(p: string): string {
  const parts = p.split("/");
  if (parts.length <= 3) return p;
  return ".../" + parts.slice(-2).join("/");
}

export function TaskTranscriptTimeline({
  items,
  isLive,
  className,
  emptyLabel = "No events yet.",
  liveEmptyLabel = "Waiting for events...",
}: TaskTranscriptTimelineProps) {
  if (items.length === 0) {
    return (
      <div className={cn("flex items-center justify-center text-sm text-muted-foreground py-8", className)}>
        {isLive ? liveEmptyLabel : emptyLabel}
      </div>
    );
  }

  return (
    <div className={cn("divide-y", className)}>
      {items.map((item) => (
        <TranscriptEventRow key={item.seq} item={item} />
      ))}
    </div>
  );
}

interface TranscriptEventRowProps {
  item: TimelineItem;
}

function TranscriptEventRow({ item }: TranscriptEventRowProps) {
  const [expanded, setExpanded] = useState(false);
  const color = getEventColor(item);
  const label = getEventLabel(item);
  const summary = getEventSummary(item);

  const hasDetail =
    (item.type === "tool_use" && item.input && Object.keys(item.input).length > 0) ||
    (item.type === "tool_result" && item.output && item.output.length > 0) ||
    (item.type === "thinking" && item.content && item.content.length > 0) ||
    (item.type === "text" && item.content && item.content.length > 0) ||
    (item.type === "error" && item.content && item.content.length > 0);

  return (
    <div className="group transition-colors">
      <Collapsible open={expanded} onOpenChange={setExpanded}>
        <div className="flex items-start gap-2 px-4 py-2">
          <span
            className={cn(
              "inline-flex items-center shrink-0 rounded px-1.5 py-0.5 text-[11px] font-medium mt-0.5 min-w-[60px] justify-center",
              colorClasses[color].label,
            )}
          >
            {item.type === "thinking" && <Brain className="h-3 w-3 mr-1 shrink-0" />}
            {item.type === "error" && <AlertCircle className="h-3 w-3 mr-1 shrink-0" />}
            {label}
          </span>

          <CollapsibleTrigger
            className={cn(
              "flex-1 text-left text-xs min-w-0 py-0.5 transition-colors",
              hasDetail ? "cursor-pointer hover:text-foreground" : "cursor-default",
              item.type === "error" ? "text-destructive" : "text-muted-foreground",
            )}
            disabled={!hasDetail}
          >
            <div className="flex items-start gap-1.5">
              {hasDetail && (
                <ChevronRight
                  className={cn(
                    "h-3 w-3 shrink-0 mt-0.5 text-muted-foreground/50 transition-transform",
                    expanded && "rotate-90",
                  )}
                />
              )}
              <span className="truncate">{summary || "(empty)"}</span>
            </div>
          </CollapsibleTrigger>

          <span className="shrink-0 text-[10px] text-muted-foreground/50 tabular-nums mt-1">
            #{item.seq}
          </span>
        </div>

        {hasDetail && (
          <CollapsibleContent>
            <div className="px-4 pb-3">
              <div className="ml-[72px] rounded bg-muted/40 border">
                <EventDetailContent item={item} />
              </div>
            </div>
          </CollapsibleContent>
        )}
      </Collapsible>
    </div>
  );
}

function EventDetailContent({ item }: { item: TimelineItem }) {
  switch (item.type) {
    case "tool_use":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.input ? redactSecrets(JSON.stringify(item.input, null, 2)) : ""}
        </pre>
      );
    case "tool_result":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-all">
          {item.output
            ? item.output.length > 4000
              ? redactSecrets(item.output.slice(0, 4000)) + "\n... (truncated)"
              : redactSecrets(item.output)
            : ""}
        </pre>
      );
    case "thinking":
    case "text":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-muted-foreground whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    case "error":
      return (
        <pre className="max-h-60 overflow-auto p-3 text-[11px] text-destructive whitespace-pre-wrap break-words">
          {item.content ?? ""}
        </pre>
      );
    default:
      return null;
  }
}
```

### Step 4: Export from the task-transcript index

Modify `packages/views/common/task-transcript/index.ts`:

```ts
export { AgentTranscriptDialog } from "./agent-transcript-dialog";
export { TranscriptButton } from "./transcript-button";
export { buildTimeline, type TimelineItem } from "./build-timeline";
export { redactSecrets } from "./redact";
export { TaskTranscriptTimeline } from "./task-transcript-timeline";
```

### Step 5: Run the test to verify it passes

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/task-transcript-timeline.test.tsx
```

Expected: PASS.

### Step 6: Commit

```bash
git add packages/views/common/task-transcript/
git commit -m "feat(views): extract TaskTranscriptTimeline component

Move event-row rendering out of AgentTranscriptDialog so it can be
reused by the inline execution-log transcript panel.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 2: Refactor `AgentTranscriptDialog` to use `TaskTranscriptTimeline`

**Files:**
- Modify: `packages/views/common/task-transcript/agent-transcript-dialog.tsx`
- Create: `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`

### Step 1: Write the failing test

Create `packages/views/common/task-transcript/agent-transcript-dialog.test.tsx`:

```tsx
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { AgentTranscriptDialog } from "./agent-transcript-dialog";
import type { AgentTask } from "@multica/core/types/agent";
import type { TimelineItem } from "./build-timeline";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { agents: enAgents } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      {children}
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  status: "completed",
  created_at: new Date().toISOString(),
} as AgentTask;

const items: TimelineItem[] = [
  { seq: 1, type: "text", content: "Done" },
];

describe("AgentTranscriptDialog", () => {
  it("renders timeline items through the shared component", () => {
    render(
      <QueryClientProvider client={new QueryClient()}>
        <AgentTranscriptDialog open task={task} items={items} agentName="Test Agent" />
      </QueryClientProvider>,
      { wrapper: Wrapper },
    );
    expect(screen.getByText("Agent")).toBeInTheDocument();
    expect(screen.getByText("Done")).toBeInTheDocument();
  });
});
```

### Step 2: Run the test to verify it fails

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/agent-transcript-dialog.test.tsx
```

Expected: FAIL because `AgentTranscriptDialog` still imports removed helpers.

### Step 3: Refactor `AgentTranscriptDialog`

In `packages/views/common/task-transcript/agent-transcript-dialog.tsx`:

1. Remove imports that moved: `Brain`, `AlertCircle`, `ChevronRight` are no longer needed for event rows (keep `ChevronRight` if used elsewhere? It's only used in `TranscriptEventRow`, so remove it). Remove `Collapsible`, `CollapsibleContent`, `CollapsibleTrigger` imports.
2. Remove local functions/types: `getEventColor`, `getEventLabel`, `getEventSummary`, `colorClasses`, `shortenPath`, `TranscriptEventRow`, `EventDetailContent`.
3. Import `TaskTranscriptTimeline` from `./task-transcript-timeline`.
4. Replace the event list block (lines ~506–537) with:

```tsx
        {/* ── Event list ─────────────────────────── */}
        <div
          ref={scrollContainerRef}
          className="flex-1 overflow-y-auto min-h-0"
        >
          <TaskTranscriptTimeline
            items={displayItems}
            isLive={isLive}
            className="h-full"
            emptyLabel={t(($) => $.transcript.no_data)}
            liveEmptyLabel={t(($) => $.transcript.waiting_events)}
          />
        </div>
```

### Step 4: Run the test to verify it passes

```bash
pnpm --filter @multica/views exec vitest run common/task-transcript/agent-transcript-dialog.test.tsx
```

Expected: PASS.

### Step 5: Commit

```bash
git add packages/views/common/task-transcript/
git commit -m "refactor(views): AgentTranscriptDialog uses TaskTranscriptTimeline

Removes duplicated event-row rendering and imports the shared timeline.
No user-facing behavior change.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 3: Add locale strings

**Files:**
- Modify: `packages/views/locales/en/issues.json`
- Modify: `packages/views/locales/zh-Hans/issues.json`

### Step 1: Add English strings

In `packages/views/locales/en/issues.json`, inside the existing `execution_log` object, add:

```json
    "show_transcript": "Show transcript",
    "hide_transcript": "Hide transcript",
    "live_indicator": "Live",
    "no_events_yet": "No events yet.",
    "waiting_events": "Waiting for events...",
    "show_thinking": "Show thinking",
    "transcript_error": "Failed to load transcript",
    "retry": "Retry"
```

### Step 2: Add Chinese strings

In `packages/views/locales/zh-Hans/issues.json`, inside the existing `execution_log` object, add the equivalents:

```json
    "show_transcript": "查看对话流",
    "hide_transcript": "隐藏对话流",
    "live_indicator": "实时",
    "no_events_yet": "暂无事件",
    "waiting_events": "等待事件中...",
    "show_thinking": "显示思考过程",
    "transcript_error": "加载对话流失败",
    "retry": "重试"
```

### Step 3: Run locale parity test

```bash
pnpm --filter @multica/views exec vitest run locales/parity.test.ts
```

Expected: PASS. If it fails, ensure both JSON files have identical key sets.

### Step 4: Commit

```bash
git add packages/views/locales/
git commit -m "i18n(issues): add inline transcript strings

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 4: Create `InlineTranscriptPanel`

**Files:**
- Create: `packages/views/issues/components/execution-log/inline-transcript-panel.tsx`
- Create: `packages/views/issues/components/execution-log/inline-transcript-panel.test.tsx`

### Step 1: Write the failing test

Create `packages/views/issues/components/execution-log/inline-transcript-panel.test.tsx`:

```tsx
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import { render, screen, waitFor, fireEvent } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { InlineTranscriptPanel } from "./inline-transcript-panel";
import type { AgentTask } from "@multica/core/types/agent";
import type { TaskMessagePayload } from "@multica/core/types/events";
import enIssues from "../../../locales/en/issues.json";

const mockListTaskMessages = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: { listTaskMessages: mockListTaskMessages },
}));

const TEST_RESOURCES = { en: { issues: enIssues } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  status: "running",
  created_at: new Date().toISOString(),
  started_at: new Date().toISOString(),
} as AgentTask;

const messages: TaskMessagePayload[] = [
  { task_id: "task-1", issue_id: "issue-1", seq: 1, type: "text", content: "Hello" },
];

describe("InlineTranscriptPanel", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListTaskMessages.mockResolvedValue(messages);
  });

  it("expands to show fetched messages", async () => {
    render(<InlineTranscriptPanel task={task} isLive defaultOpen={false} />, { wrapper: Wrapper });
    await userEvent.click(screen.getByRole("button", { name: /show transcript/i }));
    await waitFor(() => expect(screen.getByText("Hello")).toBeInTheDocument());
  });

  it("shows live indicator when running", async () => {
    render(<InlineTranscriptPanel task={task} isLive defaultOpen />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByText("Live")).toBeInTheDocument());
  });
});
```

### Step 2: Run the test to verify it fails

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution-log/inline-transcript-panel.test.tsx
```

Expected: FAIL because the component does not exist.

### Step 3: Implement `InlineTranscriptPanel`

Create `packages/views/issues/components/execution-log/inline-transcript-panel.tsx`:

```tsx
"use client";

import { useState, useRef, useEffect, useCallback } from "react";
import { useQuery } from "@tanstack/react-query";
import { ChevronRight, Loader2, AlertCircle, RotateCcw } from "lucide-react";
import { cn } from "@multica/ui/lib/utils";
import { Button } from "@multica/ui/components/ui/button";
import { api } from "@multica/core/api";
import { taskMessagesOptions } from "@multica/core/chat/queries";
import type { AgentTask } from "@multica/core/types/agent";
import { TaskTranscriptTimeline, buildTimeline, type TimelineItem } from "../../../common/task-transcript";
import { useT } from "../../i18n";

interface InlineTranscriptPanelProps {
  task: AgentTask;
  isLive?: boolean;
  defaultOpen?: boolean;
}

export function InlineTranscriptPanel({ task, isLive, defaultOpen = false }: InlineTranscriptPanelProps) {
  const { t } = useT("issues");
  const [open, setOpen] = useState(defaultOpen);
  const [showThinking, setShowThinking] = useState(true);
  const scrollRef = useRef<HTMLDivElement>(null);
  const wasNearBottomRef = useRef(true);

  const { data: messages = [], isLoading, error, refetch } = useQuery({
    ...taskMessagesOptions(task.id),
    enabled: open,
  });

  const items = buildTimeline(messages).filter((item) => (showThinking ? true : item.type !== "thinking"));

  const scrollToBottom = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    el.scrollTop = el.scrollHeight;
  }, []);

  useEffect(() => {
    if (!open || !isLive) return;
    if (wasNearBottomRef.current) {
      scrollToBottom();
    }
  }, [items, open, isLive, scrollToBottom]);

  const handleScroll = useCallback(() => {
    const el = scrollRef.current;
    if (!el) return;
    const nearBottom = el.scrollHeight - el.scrollTop - el.clientHeight < 40;
    wasNearBottomRef.current = nearBottom;
  }, []);

  const isRunning = isLive && task.status === "running";

  return (
    <div className="mt-1">
      <button
        type="button"
        onClick={() => setOpen((v) => !v)}
        className="flex items-center gap-1 text-xs text-muted-foreground hover:text-foreground transition-colors"
      >
        <ChevronRight className={cn("!size-3 shrink-0 stroke-[2.5] transition-transform", open && "rotate-90")} />
        {open ? t(($) => $.execution_log.hide_transcript) : t(($) => $.execution_log.show_transcript)}
        {isRunning && (
          <span className="ml-1 inline-flex items-center gap-1 text-info">
            <span className="h-1.5 w-1.5 rounded-full bg-info animate-pulse" />
            {t(($) => $.execution_log.live_indicator)}
          </span>
        )}
      </button>

      {open && (
        <div className="mt-1 rounded-md border bg-muted/20">
          <div className="flex items-center justify-between px-2 py-1 border-b">
            <label className="flex items-center gap-1.5 text-xs text-muted-foreground cursor-pointer">
              <input
                type="checkbox"
                checked={showThinking}
                onChange={(e) => setShowThinking(e.target.checked)}
                className="h-3 w-3 rounded border-muted-foreground/30"
              />
              {t(($) => $.execution_log.show_thinking)}
            </label>
          </div>

          {isLoading ? (
            <div className="flex items-center justify-center gap-2 py-8 text-xs text-muted-foreground">
              <Loader2 className="h-3.5 w-3.5 animate-spin" />
              Loading...
            </div>
          ) : error ? (
            <div className="flex flex-col items-center justify-center gap-2 py-8 text-xs text-destructive">
              <AlertCircle className="h-4 w-4" />
              {t(($) => $.execution_log.transcript_error)}
              <Button variant="ghost" size="sm" onClick={() => refetch()} className="h-6 gap-1 text-xs">
                <RotateCcw className="h-3 w-3" />
                {t(($) => $.execution_log.retry)}
              </Button>
            </div>
          ) : (
            <div
              ref={scrollRef}
              onScroll={handleScroll}
              className="max-h-80 overflow-y-auto"
            >
              <TaskTranscriptTimeline
                items={items}
                isLive={isRunning}
                emptyLabel={t(($) => $.execution_log.no_events_yet)}
                liveEmptyLabel={t(($) => $.execution_log.waiting_events)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}
```

### Step 4: Run the test to verify it passes

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution-log/inline-transcript-panel.test.tsx
```

Expected: PASS.

### Step 5: Commit

```bash
git add packages/views/issues/components/execution-log/
git commit -m "feat(views): add InlineTranscriptPanel

Expandable live transcript panel for a single task run. Fetches
messages via taskMessagesOptions and renders them with
TaskTranscriptTimeline. Supports live indicator and auto-scroll.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 5: Wire `InlineTranscriptPanel` into `ExecutionLogSection`

**Files:**
- Modify: `packages/views/issues/components/execution-log-section.tsx`
- Create: `packages/views/issues/components/execution-log-section.test.tsx`

### Step 1: Write the failing test

Create `packages/views/issues/components/execution-log-section.test.tsx`:

```tsx
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { I18nProvider } from "@multica/core/i18n/react";
import { ExecutionLogSection } from "./execution-log-section";
import type { AgentTask } from "@multica/core/types/agent";
import enIssues from "../locales/en/issues.json";

const mockListTasksByIssue = vi.hoisted(() => vi.fn());
const mockListTaskMessages = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", () => ({
  api: {
    listTasksByIssue: mockListTasksByIssue,
    listTaskMessages: mockListTaskMessages,
    cancelTask: vi.fn(),
    rerunIssue: vi.fn(),
  },
}));

const TEST_RESOURCES = { en: { issues: enIssues } };

function Wrapper({ children }: { children: ReactNode }) {
  return (
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={new QueryClient()}>{children}</QueryClientProvider>
    </I18nProvider>
  );
}

const task = {
  id: "task-1",
  issue_id: "issue-1",
  agent_id: "agent-1",
  status: "running",
  created_at: new Date().toISOString(),
  started_at: new Date().toISOString(),
} as AgentTask;

describe("ExecutionLogSection", () => {
  it("renders an inline transcript toggle for an active task", async () => {
    mockListTasksByIssue.mockResolvedValue([task]);
    mockListTaskMessages.mockResolvedValue([]);
    render(<ExecutionLogSection issueId="issue-1" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByRole("button", { name: /show transcript/i })).toBeInTheDocument());
  });

  it("expands the inline panel when clicked", async () => {
    mockListTasksByIssue.mockResolvedValue([task]);
    mockListTaskMessages.mockResolvedValue([]);
    render(<ExecutionLogSection issueId="issue-1" />, { wrapper: Wrapper });
    await waitFor(() => expect(screen.getByRole("button", { name: /show transcript/i })).toBeInTheDocument());
    await userEvent.click(screen.getByRole("button", { name: /show transcript/i }));
    await waitFor(() => expect(screen.getByText("Waiting for events...")).toBeInTheDocument());
  });
});
```

### Step 2: Run the test to verify it fails

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution-log-section.test.tsx
```

Expected: FAIL because the component does not render `InlineTranscriptPanel` yet.

### Step 3: Modify `ExecutionLogSection`

In `packages/views/issues/components/execution-log-section.tsx`:

1. Import `InlineTranscriptPanel` from `./execution-log/inline-transcript-panel`.
2. In `ActiveRow`, after `</RowShell>` or inside `RowShell`, render:

```tsx
      {showTranscript && (
        <InlineTranscriptPanel task={task} isLive defaultOpen />
      )}
```

Wait — `RowShell` currently wraps the row content. The panel should appear below the row, so it needs to be inside `RowShell` or immediately after it. Check `RowShell` to decide. If `RowShell` is a block container, place the panel as the last child inside it.

For `PastRow`, add:

```tsx
      <InlineTranscriptPanel task={task} isLive={false} defaultOpen={false} />
```

Also, the existing `TranscriptButton` in `ActiveRow` can optionally be kept (some users may still prefer the modal), or replaced. The design says the inline panel is additive and the modal is preserved. Keep the `TranscriptButton`.

### Step 4: Run the test to verify it passes

```bash
pnpm --filter @multica/views exec vitest run issues/components/execution-log-section.test.tsx
```

Expected: PASS.

### Step 5: Commit

```bash
git add packages/views/issues/components/execution-log-section.tsx
git add packages/views/issues/components/execution-log-section.test.tsx
git commit -m "feat(issues): embed InlineTranscriptPanel in ExecutionLogSection

Active and past task rows now expose an expandable inline transcript
panel alongside the existing modal transcript button.

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Task 6: Verification

### Step 1: TypeScript check

```bash
pnpm typecheck
```

Expected: no errors.

### Step 2: Run all `@multica/views` tests

```bash
pnpm --filter @multica/views exec vitest run
```

Expected: all tests pass.

### Step 3: Run the full project check (optional but recommended)

```bash
make check
```

Expected: passes. If E2E tests fail due to environment, address backend/frontend startup first.

### Step 4: Commit any fixes

If any fixes were required:

```bash
git add -A
git commit -m "fix(views): address inline transcript review findings

Co-Authored-By: Claude <noreply@anthropic.com>"
```

---

## Self-review

**1. Spec coverage:**

- Extract shared timeline renderer → Task 1.
- Reuse modal transcript body → Task 2.
- Inline panel with query + live indicator + auto-scroll → Task 4.
- Embed in ExecutionLogSection active/past rows → Task 5.
- i18n strings → Task 3.
- Tests → each task has component tests; Task 6 runs full verification.

**2. Placeholder scan:**

- No TBD/TODO/fill-in-details.
- All code blocks are concrete.
- File paths are exact.

**3. Type consistency:**

- `TimelineItem` type imported from `build-timeline` everywhere.
- `TaskMessagePayload` type used for mocks matches `@multica/core/types/events`.
- `AgentTask` type used for fixtures matches `@multica/core/types/agent`.

**4. One known refinement needed at execution time:**

- Verify `RowShell` is a block container before placing `InlineTranscriptPanel` inside it; if it wraps only the single row line, place the panel immediately after `</RowShell>` in `ActiveRow`/`PastRow` instead.
