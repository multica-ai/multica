/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { chatKeys, taskMessagesOptions } from "../chat/queries";
import type { TaskMessagePayload } from "../types/events";
import type { WSClient } from "../api/ws-client";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

const HELD_TASK = "11111111-1111-4111-8111-111111111111";
const UNHELD_TASK = "22222222-2222-4222-8222-222222222222";
const FLUSH_MS = 100;

type Handlers = Map<string, (payload: unknown) => void>;

function createMockWs(handlers: Handlers): WSClient {
  return {
    on: vi.fn((event: string, handler: (payload: unknown) => void) => {
      handlers.set(event, handler);
      return () => handlers.delete(event);
    }),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "u1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

function msg(taskId: string, seq: number, extra: Partial<TaskMessagePayload> = {}): TaskMessagePayload {
  return {
    task_id: taskId,
    issue_id: "issue-1",
    seq,
    type: "tool_use",
    ...extra,
  };
}

function cached(qc: QueryClient, taskId: string) {
  return qc.getQueryData<TaskMessagePayload[]>(chatKeys.taskMessages(taskId));
}

describe("useRealtimeSync — task:message fanout guards (MUL-6396)", () => {
  let qc: QueryClient;
  let handlers: Handlers;
  let listTaskMessages: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    vi.useFakeTimers();
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    handlers = new Map();
    listTaskMessages = vi.fn(async () => [] as TaskMessagePayload[]);
    setApiInstance({ listTaskMessages } as unknown as ApiClient);
  });

  afterEach(() => {
    vi.useRealTimers();
    setApiInstance(undefined as unknown as ApiClient);
  });

  function mount() {
    renderHook(() => useRealtimeSync(createMockWs(handlers), createStores()), {
      wrapper: createWrapper(qc),
    });
    const handler = handlers.get("task:message");
    if (!handler) throw new Error("task:message handler was not registered");
    return handler;
  }

  /** Simulates a mounted view rendering this task's timeline. */
  function holdTimeline(taskId: string) {
    const observer = new QueryObserver(qc, taskMessagesOptions(taskId));
    const unsubscribe = observer.subscribe(() => {});
    return unsubscribe;
  }

  it("drops frames for a task no mounted view is rendering", () => {
    const handler = mount();

    handler(msg(UNHELD_TASK, 1));
    handler(msg(UNHELD_TASK, 2));
    vi.advanceTimersByTime(FLUSH_MS * 2);

    // The whole point: a run the user never opened must not build a cache
    // entry, however long it streams.
    expect(cached(qc, UNHELD_TASK)).toBeUndefined();
    expect(qc.getQueryCache().find({ queryKey: chatKeys.taskMessages(UNHELD_TASK) })).toBeUndefined();
  });

  it("keeps frames for a task a mounted view holds, even before its fetch resolves", () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);

    // Mounting registers the cache entry immediately; the queryFn above has
    // not resolved yet. A frame landing in that window must still be kept.
    handler(msg(HELD_TASK, 1));
    vi.advanceTimersByTime(FLUSH_MS);

    expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1]);
    release();
  });

  it("coalesces a burst into a single cache write", () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    const writes = vi.spyOn(qc, "setQueryData");

    for (let seq = 1; seq <= 5; seq++) handler(msg(HELD_TASK, seq));
    // Nothing is written until the window closes.
    expect(writes).not.toHaveBeenCalled();

    vi.advanceTimersByTime(FLUSH_MS);

    expect(writes).toHaveBeenCalledTimes(1);
    expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1, 2, 3, 4, 5]);
    release();
  });

  it("flushes on a fixed window rather than being starved by a continuous stream", () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);

    // A frame every 50ms: a resetting debounce would never fire.
    for (let seq = 1; seq <= 4; seq++) {
      handler(msg(HELD_TASK, seq));
      vi.advanceTimersByTime(FLUSH_MS / 2);
    }

    expect(cached(qc, HELD_TASK)?.length).toBeGreaterThan(0);
    release();
  });

  it("keeps live frames that landed while the first fetch was still in flight", async () => {
    // The regression P0-a newly exposes. Before it, the cache was pre-seeded by
    // the WS handler, so opening a live task found fresh data (staleTime:
    // Infinity) and never fetched. Now first open DOES fetch, and a response
    // that resolves after a live frame was written must not drop that seq —
    // nothing would ever refetch it.
    let resolveFetch: (msgs: TaskMessagePayload[]) => void = () => {};
    listTaskMessages.mockImplementation(
      () => new Promise<TaskMessagePayload[]>((resolve) => { resolveFetch = resolve; }),
    );

    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(listTaskMessages).toHaveBeenCalled());

    // Live frame arrives and flushes while the request is still open.
    handler(msg(HELD_TASK, 2, { content: "live" }));
    vi.advanceTimersByTime(FLUSH_MS);
    expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([2]);

    // The response was snapshotted before seq 2 was persisted.
    resolveFetch([msg(HELD_TASK, 1, { content: "persisted" })]);

    await vi.waitFor(() => {
      expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1, 2]);
    });
    expect(cached(qc, HELD_TASK)?.[1]?.content).toBe("live");
    release();
  });

  it("drops a batch whose timeline was garbage-collected during the flush window", async () => {
    // Production shape: app-wide staleTime Infinity, and a gcTime short enough
    // to land inside the 100ms batching window. Writes do NOT postpone the GC
    // timer (query-core arms it when the last observer leaves), so a run that
    // keeps streaming after its transcript is closed reaches this every time.
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: 50 } },
    });
    listTaskMessages.mockResolvedValue([msg(HELD_TASK, 1, { content: "history" })]);

    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1]));

    // Frame batched while the entry is still held, then the viewer closes.
    handler(msg(HELD_TASK, 2, { content: "live" }));
    release();

    // GC lands first (50ms), flush second (100ms).
    vi.advanceTimersByTime(60);
    expect(qc.getQueryCache().find({ queryKey: chatKeys.taskMessages(HELD_TASK) })).toBeUndefined();
    vi.advanceTimersByTime(60);

    // Writing would have rebuilt the entry holding ONLY seq 2. Under
    // staleTime: Infinity the next open would read that stub as fresh and
    // never fetch, losing seq 1 until the window is reloaded.
    expect(qc.getQueryCache().find({ queryKey: chatKeys.taskMessages(HELD_TASK) })).toBeUndefined();

    // And the history is still recoverable: reopening fetches the full timeline.
    listTaskMessages.mockClear();
    const reopen = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(listTaskMessages).toHaveBeenCalledTimes(1));
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1]));
    reopen();
  });
});
