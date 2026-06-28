// @vitest-environment jsdom
//
// CEREBRO regression guard for FIR-2173.
//
// The inbox "running" marker (inboxKeys.activeIssueTasks) and the inbox wakeup
// list (["cerebro-inbox-wakeups"]) are refreshed live by the onAny `task:`
// prefix handler inside useRealtimeSync. A previous upstream-sync refactor
// (MUL-3375) inlined that handler and silently dropped the two cerebro
// invalidations, so a started agent no longer flipped its inbox row to
// "running" or re-categorised live. The existing unit test only exercised the
// standalone invalidateTaskLifecycleQueries() helper — which is NOT wired into
// the live dispatch — so it stayed green while production was broken.
//
// This test drives the REAL path: it renders useRealtimeSync, captures the
// onAny handler the hook registers on the WS client, fires a `task:running`
// event, and asserts both cerebro inbox queries are invalidated.

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import { createElement, type ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { inboxKeys } from "../inbox/queries";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import type { WSClient } from "../api/ws-client";
import type { WSMessage } from "../types/events";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

// useHasOnboarded() reads the platform-registered global auth store, which is
// not wired up in a unit test. Stub the paths module so the hook has no global
// store dependency — the task path under test never touches onboarding.
vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => null,
}));

const wsId = "ws-task-dispatch";

function createFakeWs() {
  let anyHandler: ((msg: WSMessage) => void) | null = null;
  const ws = {
    on: () => () => {},
    onAny: (handler: (msg: WSMessage) => void) => {
      anyHandler = handler;
      return () => {
        anyHandler = null;
      };
    },
    onReconnect: () => () => {},
    send: () => {},
  } as unknown as WSClient;
  return {
    ws,
    emit: (msg: WSMessage) => anyHandler?.(msg),
  };
}

const stores: RealtimeSyncStores = {
  authStore: Object.assign(() => undefined, {
    getState: () => ({ user: { id: "u1" } }),
  }) as unknown as RealtimeSyncStores["authStore"],
};

function wrapper(qc: QueryClient) {
  return ({ children }: { children: ReactNode }) =>
    createElement(QueryClientProvider, { client: qc }, children);
}

describe("useRealtimeSync onAny task: dispatch (FIR-2173)", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    setCurrentWorkspace("ws-slug", wsId);
  });

  afterEach(() => {
    vi.useRealTimers();
    setCurrentWorkspace(null, null);
  });

  it("invalidates the inbox active-run marker and wakeup list on a task lifecycle event", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const activeIssueTasksKey = inboxKeys.activeIssueTasks(wsId);
    const wakeupKey = ["cerebro-inbox-wakeups", wsId] as const;
    qc.setQueryData(activeIssueTasksKey, { issue_ids: [] });
    qc.setQueryData(wakeupKey, []);

    const { emit } = createFakeWsAndRender(qc);

    emit({ type: "task:running" } as WSMessage);
    // refreshMap is debounced by 100ms.
    vi.advanceTimersByTime(150);

    expect(qc.getQueryState(activeIssueTasksKey)?.isInvalidated).toBe(true);
    expect(qc.getQueryState(wakeupKey)?.isInvalidated).toBe(true);
  });

  function createFakeWsAndRender(qc: QueryClient) {
    const fake = createFakeWs();
    renderHook(() => useRealtimeSync(fake.ws, stores), {
      wrapper: wrapper(qc),
    });
    return fake;
  }
});
