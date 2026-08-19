/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider, QueryObserver } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { backfillTaskMessages, chatKeys, taskMessagesOptions } from "../chat/queries";
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

  it("backfills the full row once per flush when the broadcast copy was truncated", async () => {
    // The mounted view's own fetch finds nothing yet, so the ONLY path to the
    // full text is the backfill this test is about.
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    // Let the view's own fetch settle before touching the mock: a resolving
    // queryFn REPLACES the cache, so an in-flight one would clobber the frames.
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)).toEqual([]));
    listTaskMessages.mockClear();
    listTaskMessages.mockResolvedValue([
      msg(HELD_TASK, 1, { input: { content: "full body" } }),
      msg(HELD_TASK, 2, { input: { content: "full body 2" } }),
    ]);

    handler(msg(HELD_TASK, 1, { input: { content: "clip" }, truncated: true }));
    handler(msg(HELD_TASK, 2, { input: { content: "clip" }, truncated: true }));
    vi.advanceTimersByTime(FLUSH_MS);

    // Two truncated frames in one window cost one refetch, not two.
    expect(listTaskMessages).toHaveBeenCalledTimes(1);
    // The clipped copy is rendered immediately — the backfill is a repair pass,
    // not a gate on showing the row at all.
    expect(cached(qc, HELD_TASK)?.[0]?.input).toEqual({ content: "clip" });

    await vi.waitFor(() => {
      // mergeTaskMessagesBySeq lets the cached entry win on conflict, so the
      // backfill has to evict the clipped rows or they would be pinned forever.
      expect(cached(qc, HELD_TASK)?.map((m) => m.input)).toEqual([
        { content: "full body" },
        { content: "full body 2" },
      ]);
      expect(cached(qc, HELD_TASK)?.some((m) => m.truncated)).toBe(false);
    });
    release();
  });

  it("leaves a clipped row that the backfill response did not cover", async () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)).toEqual([]));
    listTaskMessages.mockClear();
    // A response snapshotted before seq 2 was persisted — the racing backfill
    // of a later flush must not delete a row it simply has not seen yet.
    listTaskMessages.mockResolvedValue([msg(HELD_TASK, 1, { input: { content: "full body" } })]);

    handler(msg(HELD_TASK, 1, { input: { content: "clip" }, truncated: true }));
    handler(msg(HELD_TASK, 2, { input: { content: "clip 2" }, truncated: true }));
    vi.advanceTimersByTime(FLUSH_MS);

    await vi.waitFor(() => {
      expect(cached(qc, HELD_TASK)?.[0]?.input).toEqual({ content: "full body" });
    });
    // seq 2 survives, still clipped, waiting for the next backfill.
    expect(cached(qc, HELD_TASK)?.map((m) => m.seq)).toEqual([1, 2]);
    expect(cached(qc, HELD_TASK)?.[1]?.input).toEqual({ content: "clip 2" });
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

  it("lets the fetched row win over a clipped one for the same seq", async () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)).toEqual([]));

    handler(msg(HELD_TASK, 1, { input: { content: "clip" }, truncated: true }));
    vi.advanceTimersByTime(FLUSH_MS);
    // Server data is authoritative: the clipped copy must not survive.
    expect(cached(qc, HELD_TASK)?.[0]?.truncated).toBe(true);

    listTaskMessages.mockResolvedValue([msg(HELD_TASK, 1, { input: { content: "full" } })]);
    await backfillTaskMessages(qc, HELD_TASK);

    expect(cached(qc, HELD_TASK)?.[0]?.input).toEqual({ content: "full" });
    expect(cached(qc, HELD_TASK)?.[0]?.truncated).toBeUndefined();
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

  it("does not backfill a timeline that was collected while the batch waited", async () => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity, gcTime: 50 } },
    });
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    await vi.waitFor(() => expect(cached(qc, HELD_TASK)).toEqual([]));
    listTaskMessages.mockClear();

    handler(msg(HELD_TASK, 1, { input: { content: "clip" }, truncated: true }));
    release();
    vi.advanceTimersByTime(120);

    // The batch was dropped, so its repair fetch must be dropped with it —
    // otherwise the response would rebuild the very entry GC just removed.
    expect(listTaskMessages).not.toHaveBeenCalled();
    expect(qc.getQueryCache().find({ queryKey: chatKeys.taskMessages(HELD_TASK) })).toBeUndefined();
  });

  it("does not rebuild a collected timeline when a backfill response lands late", async () => {
    // The same invariant on the async side: the entry can go away while the
    // repair request is in flight.
    listTaskMessages.mockResolvedValue([msg(HELD_TASK, 1)]);

    await backfillTaskMessages(qc, HELD_TASK);

    expect(qc.getQueryCache().find({ queryKey: chatKeys.taskMessages(HELD_TASK) })).toBeUndefined();
  });

  it("does not refetch when nothing was truncated", () => {
    const handler = mount();
    const release = holdTimeline(HELD_TASK);
    listTaskMessages.mockClear();

    handler(msg(HELD_TASK, 1));
    vi.advanceTimersByTime(FLUSH_MS);

    expect(listTaskMessages).not.toHaveBeenCalled();
    release();
  });
});
