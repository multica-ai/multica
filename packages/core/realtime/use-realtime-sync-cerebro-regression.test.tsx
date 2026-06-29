/**
 * @vitest-environment jsdom
 */
// CEREBRO-PATCH(realtime-regression-test): guards the cerebro realtime wiring an
// upstream cherry-pick (#1856) silently removed (FIR-2215) — registerCerebroHandlers,
// the inbox run-pip + wakeup invalidation on task events, and the channel-unread
// invalidation on comment events. If a future upstream merge drops them again,
// these tests fail instead of shipping a silently-broken inbox.
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WSClient } from "../api/ws-client";
import { inboxKeys } from "../inbox/queries";
import { channelKeys } from "../channels/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

const wsId = "ws-1";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => wsId,
  getCurrentSlug: () => "test-ws",
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

// vi.hoisted so the spy exists when the (hoisted) vi.mock factory runs.
const { registerCerebroHandlersMock } = vi.hoisted(() => ({
  registerCerebroHandlersMock: vi.fn(() => vi.fn()),
}));
vi.mock("@multica/cerebro-realtime", () => ({
  registerCerebroHandlers: registerCerebroHandlersMock,
}));

type Handler = (payload: unknown) => void;

function createMockWs(): {
  ws: WSClient;
  handlers: Map<string, Handler>;
  anyHandlers: Handler[];
} {
  const handlers = new Map<string, Handler>();
  const anyHandlers: Handler[] = [];
  const ws = {
    on: vi.fn((type: string, handler: Handler) => {
      handlers.set(type, handler);
      return () => {};
    }),
    onAny: vi.fn((handler: Handler) => {
      anyHandlers.push(handler);
      return () => {};
    }),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
  return { ws, handlers, anyHandlers };
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
  // Named function (not arrow) so the react/display-name lint rule passes.
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useRealtimeSync — cerebro realtime regression guards (FIR-2215)", () => {
  let qc: QueryClient;

  beforeEach(() => {
    registerCerebroHandlersMock.mockClear();
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("registers cerebro WS handlers on mount and tears them down on unmount", () => {
    const teardown = vi.fn();
    registerCerebroHandlersMock.mockReturnValueOnce(teardown);
    const { ws } = createMockWs();

    const { unmount } = renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });

    expect(registerCerebroHandlersMock).toHaveBeenCalledTimes(1);
    expect(registerCerebroHandlersMock).toHaveBeenCalledWith(ws, qc);

    unmount();
    expect(teardown).toHaveBeenCalledTimes(1);
  });

  it("invalidates the inbox run-pip + wakeup queries on a task lifecycle event", () => {
    vi.useFakeTimers();
    const { ws, anyHandlers } = createMockWs();

    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });

    // Seed the two inbox queries that drive the run-pip + wakeup pin so we can
    // observe them being invalidated.
    qc.setQueryData(inboxKeys.activeIssueTasks(wsId), { tasks: [] });
    qc.setQueryData(["cerebro-inbox-wakeups", wsId], []);

    // task:completed is dispatched through the generic onAny prefix path and
    // debounced 100ms.
    expect(anyHandlers.length).toBeGreaterThan(0);
    anyHandlers.forEach((h) => h({ type: "task:completed" }));
    vi.advanceTimersByTime(150);

    expect(
      qc.getQueryState(inboxKeys.activeIssueTasks(wsId))?.isInvalidated,
    ).toBe(true);
    expect(
      qc.getQueryState(["cerebro-inbox-wakeups", wsId])?.isInvalidated,
    ).toBe(true);
  });

  it("invalidates the channel list + detail on comment:created (channel unread sync)", () => {
    const { ws, handlers } = createMockWs();

    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });

    const issueId = "issue-1";
    qc.setQueryData(channelKeys.list(wsId), []);
    qc.setQueryData(channelKeys.detail(wsId, issueId), {});

    const commentCreated = handlers.get("comment:created");
    expect(commentCreated).toBeTypeOf("function");
    commentCreated!({ comment: { issue_id: issueId } });

    expect(qc.getQueryState(channelKeys.list(wsId))?.isInvalidated).toBe(true);
    expect(
      qc.getQueryState(channelKeys.detail(wsId, issueId))?.isInvalidated,
    ).toBe(true);
  });
});
