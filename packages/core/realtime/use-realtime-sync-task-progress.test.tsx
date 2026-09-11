/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import { agentTaskSnapshotKeys } from "../agents/queries";
import type { WSMessage } from "../types/events";
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

const FLUSH_MS = 200;

function createMockWs(anyHandlers: ((msg: WSMessage) => void)[]): WSClient {
  return {
    on: vi.fn(() => () => {}),
    onAny: vi.fn((handler: (msg: WSMessage) => void) => {
      anyHandlers.push(handler);
      return () => {};
    }),
    onReconnect: vi.fn(() => () => {}),
    getLastSyncId: vi.fn(() => null),
    dispatch: vi.fn(),
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

describe("useRealtimeSync — task:progress does not trigger the task-prefix invalidation", () => {
  let qc: QueryClient;
  let anyHandlers: ((msg: WSMessage) => void)[];

  beforeEach(() => {
    vi.useFakeTimers();
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    anyHandlers = [];
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  function mount() {
    renderHook(() => useRealtimeSync(createMockWs(anyHandlers), createStores()), {
      wrapper: createWrapper(qc),
    });
    if (anyHandlers.length === 0) throw new Error("onAny handler was not registered");
  }

  function fire(type: string) {
    for (const h of anyHandlers) h({ type, payload: {} } as WSMessage);
  }

  function snapshotInvalidated(spy: { mock: { calls: unknown[][] } }) {
    return spy.mock.calls.some(([f]) => {
      const key = (f as { queryKey?: unknown[] })?.queryKey;
      return JSON.stringify(key) === JSON.stringify(agentTaskSnapshotKeys.list("ws-1"));
    });
  }

  it("ignores progress ticks: broadcast-only payload, no cache changed", () => {
    mount();
    const spy = vi.spyOn(qc, "invalidateQueries");

    // A long run's steady progress stream — this used to refetch eight query
    // families every debounce window for data that never changed.
    for (let i = 0; i < 10; i++) fire("task:progress");
    vi.advanceTimersByTime(FLUSH_MS);

    expect(snapshotInvalidated(spy)).toBe(false);
  });

  it("still refreshes the snapshot on real lifecycle transitions", () => {
    mount();
    const spy = vi.spyOn(qc, "invalidateQueries");

    fire("task:running");
    vi.advanceTimersByTime(FLUSH_MS);

    expect(snapshotInvalidated(spy)).toBe(true);
  });

  // The documented cost of this change: on main, a task:progress tick would
  // re-fetch the task caches and thereby repair a previously LOST lifecycle
  // event (cross-node publication can fail and the broadcaster ignores the
  // error, without disconnecting the browser). With progress ticks skipped,
  // a mounted view that missed task:running stays on its stale status until
  // the next event that still invalidates the task prefix — the run's own
  // completed/failed/cancelled transition. This test pins BOTH halves so the
  // tradeoff stays visible: the tick no longer fetches, the next lifecycle
  // event does.
  it("tradeoff: a dropped task:running is healed by the next lifecycle event, no longer by progress ticks", async () => {
    mount();
    let serverStatus = "queued";
    const queryFn = vi.fn(async () => ({ status: serverStatus }));
    const { result } = renderHook(
      () =>
        useQuery({
          queryKey: agentTaskSnapshotKeys.list("ws-1"),
          queryFn,
          staleTime: 30_000,
        }),
      { wrapper: createWrapper(qc) },
    );
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(result.current.data).toEqual({ status: "queued" });
    expect(queryFn).toHaveBeenCalledTimes(1);

    // The task starts server-side, but the task:running event never reaches
    // this client. The next progress tick used to trigger the repair fetch;
    // now it must not fetch at all.
    serverStatus = "running";
    fire("task:progress");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FLUSH_MS);
    });
    expect(queryFn).toHaveBeenCalledTimes(1);
    expect(result.current.data).toEqual({ status: "queued" });

    // Recovery now waits for the next event that still hits the task-prefix
    // invalidation: the run's own terminal transition.
    serverStatus = "completed";
    fire("task:completed");
    await act(async () => {
      await vi.advanceTimersByTimeAsync(FLUSH_MS);
    });
    expect(queryFn).toHaveBeenCalledTimes(2);
    expect(result.current.data).toEqual({ status: "completed" });
  });
});
