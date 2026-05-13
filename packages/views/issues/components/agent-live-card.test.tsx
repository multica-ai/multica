import { useEffect } from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { act, render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type { AgentTask } from "@multica/core/types/agent";
import enCommon from "../../locales/en/common.json";
import enIssues from "../../locales/en/issues.json";

const TEST_RESOURCES = { en: { common: enCommon, issues: enIssues } };

// ---------------------------------------------------------------------------
// Mocks
// ---------------------------------------------------------------------------

// Capture WS event handlers so the test can drive them directly. The card
// subscribes to task:queued, task:dispatch, task:completed, task:failed,
// task:cancelled, and task:message via useWSEvent. We mirror the real
// hook's useEffect-based subscription so stale subscriptions clean up
// across re-renders (otherwise every render would stack a duplicate
// handler and one event would fan out into many reconcile calls).
type EventHandler = (payload: unknown) => void;
const wsHandlers = vi.hoisted(() => new Map<string, Set<EventHandler>>());
const wsReconnectCallbacks = vi.hoisted(() => new Set<() => void>());

vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: EventHandler) => {
    useEffect(() => {
      const set = wsHandlers.get(event) ?? new Set<EventHandler>();
      set.add(handler);
      wsHandlers.set(event, set);
      return () => {
        set.delete(handler);
      };
    }, [event, handler]);
  },
  useWSReconnect: (cb: () => void) => {
    useEffect(() => {
      wsReconnectCallbacks.add(cb);
      return () => {
        wsReconnectCallbacks.delete(cb);
      };
    }, [cb]);
  },
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_: string, id: string) => (id ? `Agent ${id}` : "Agent"),
  }),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid="actor-avatar">{actorId}</span>
  ),
}));

vi.mock("../../common/task-transcript", async () => {
  const buildTimeline = vi.fn().mockReturnValue([]);
  return {
    TranscriptButton: () => <button data-testid="transcript-button">transcript</button>,
    buildTimeline,
  };
});

const mockApi = vi.hoisted(() => ({
  getActiveTasksForIssue: vi.fn(),
  listTaskMessages: vi.fn(),
  cancelTask: vi.fn(),
}));

vi.mock("@multica/core/api", () => ({
  api: mockApi,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn(), success: vi.fn() },
}));

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

import { AgentLiveCard } from "./agent-live-card";

function makeTask(id: string, overrides: Partial<AgentTask> = {}): AgentTask {
  return {
    id,
    agent_id: "agent-1",
    runtime_id: "rt-1",
    issue_id: "issue-1",
    status: "running",
    priority: 0,
    dispatched_at: "2026-01-01T00:00:00Z",
    started_at: "2026-01-01T00:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

interface Deferred<T> {
  promise: Promise<T>;
  resolve: (value: T) => void;
}

function deferred<T>(): Deferred<T> {
  let resolveFn!: (value: T) => void;
  const promise = new Promise<T>((res) => {
    resolveFn = res;
  });
  return { promise, resolve: resolveFn };
}

function fireEvent(event: string, payload: unknown) {
  const handlers = wsHandlers.get(event) ?? [];
  for (const h of handlers) h(payload);
}

function renderCard(issueId = "issue-1") {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <AgentLiveCard issueId={issueId} />
    </I18nProvider>,
  );
}

beforeEach(() => {
  wsHandlers.clear();
  wsReconnectCallbacks.clear();
  mockApi.getActiveTasksForIssue.mockReset();
  mockApi.listTaskMessages.mockReset();
  mockApi.listTaskMessages.mockResolvedValue([]);
  mockApi.cancelTask.mockReset();
});

// ---------------------------------------------------------------------------
// Tests
// ---------------------------------------------------------------------------

describe("AgentLiveCard reconcile race", () => {
  it("does not re-add a banner when an older active-task response resolves after a newer empty one", async () => {
    const mountFetch = deferred<{ tasks: AgentTask[] }>();
    const queuedFetch = deferred<{ tasks: AgentTask[] }>();
    const completedFetch = deferred<{ tasks: AgentTask[] }>();

    // The component issues three reconciles in this test:
    // 1. mount
    // 2. task:queued
    // 3. task:completed (after optimistic delete)
    // We control the order they resolve to reproduce the GPT-Boy race.
    mockApi.getActiveTasksForIssue
      .mockReturnValueOnce(mountFetch.promise)
      .mockReturnValueOnce(queuedFetch.promise)
      .mockReturnValueOnce(completedFetch.promise);

    renderCard();

    // Mount call resolves with empty — no banner yet.
    await act(async () => {
      mountFetch.resolve({ tasks: [] });
    });
    expect(screen.queryByText(/is working/)).toBeNull();

    // task:queued fires; reconcile A is now in flight (queuedFetch).
    act(() => {
      fireEvent("task:queued", { issue_id: "issue-1", task_id: "task-1" });
    });

    // task:completed fires; handler optimistically deletes (no-op since
    // the banner isn't rendered yet) then issues reconcile B (completedFetch).
    act(() => {
      fireEvent("task:completed", { issue_id: "issue-1", task_id: "task-1" });
    });

    // Reconcile B resolves first with empty list — server truth says no
    // active tasks. State is empty.
    await act(async () => {
      completedFetch.resolve({ tasks: [] });
    });
    expect(screen.queryByText(/is working/)).toBeNull();

    // Reconcile A (older, slow) resolves last with a stale snapshot that
    // still includes the task. With the generation guard, this response
    // must be dropped. Without the guard, the banner would re-appear.
    await act(async () => {
      queuedFetch.resolve({ tasks: [makeTask("task-1")] });
    });

    // The banner must NOT come back.
    expect(screen.queryByText(/is working/)).toBeNull();
    expect(mockApi.getActiveTasksForIssue).toHaveBeenCalledTimes(3);
  });

  it("WS reconnect refetch removes a stale banner whose end event was lost", async () => {
    const mountFetch = deferred<{ tasks: AgentTask[] }>();
    const reconnectFetch = deferred<{ tasks: AgentTask[] }>();

    mockApi.getActiveTasksForIssue
      .mockReturnValueOnce(mountFetch.promise)
      .mockReturnValueOnce(reconnectFetch.promise);

    renderCard();

    // Mount sees the task as active — banner shows.
    await act(async () => {
      mountFetch.resolve({ tasks: [makeTask("task-1")] });
    });
    await waitFor(() => {
      expect(screen.getByText(/is working/)).toBeTruthy();
    });

    // Simulate the WS dropping task:completed and then reconnecting.
    // The reconnect callback runs reconcile, which fetches and finds the
    // task is no longer active.
    expect(wsReconnectCallbacks.size).toBeGreaterThan(0);
    act(() => {
      for (const cb of wsReconnectCallbacks) cb();
    });

    await act(async () => {
      reconnectFetch.resolve({ tasks: [] });
    });

    // The banner self-heals.
    await waitFor(() => {
      expect(screen.queryByText(/is working/)).toBeNull();
    });
  });
});

describe("AgentLiveCard queued rendering", () => {
  it("renders 'is queued' copy without transcript when status is queued", async () => {
    const queuedTask = makeTask("task-q", {
      status: "queued",
      dispatched_at: null,
      started_at: null,
    });
    mockApi.getActiveTasksForIssue.mockResolvedValueOnce({ tasks: [queuedTask] });

    renderCard();

    await waitFor(() => {
      expect(screen.getByText(/is queued/)).toBeTruthy();
    });
    // No execution transcript while queued — no log to show yet.
    expect(screen.queryByTestId("transcript-button")).toBeNull();
    // Cancel button is still available so users can drop a queued task.
    expect(screen.getByText("Stop")).toBeTruthy();
  });

  it("running tasks sort above queued tasks so the sticky slot stays on the active one", async () => {
    const runningTask = makeTask("task-r", { status: "running" });
    const queuedTask = makeTask("task-q", {
      status: "queued",
      dispatched_at: null,
      started_at: null,
    });
    // Server returns queued first (created_at DESC), but the client must
    // re-sort so the running banner takes the sticky position.
    mockApi.getActiveTasksForIssue.mockResolvedValueOnce({
      tasks: [queuedTask, runningTask],
    });

    renderCard();

    await waitFor(() => {
      expect(screen.getByText(/is working/)).toBeTruthy();
      expect(screen.getByText(/is queued/)).toBeTruthy();
    });

    const working = screen.getByText(/is working/);
    const queued = screen.getByText(/is queued/);
    // Running banner appears earlier in the document order.
    expect(working.compareDocumentPosition(queued) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });
});
