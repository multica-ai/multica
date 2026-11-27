// @vitest-environment jsdom

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { cleanup, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import type { WSClient } from "../api/ws-client";
import { issueKeys } from "../issues/queries";
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
vi.mock("../search-index/instance", () => ({ forgetLocalSearchIndex: vi.fn(async () => undefined) }));

function createMockWs(): WSClient {
  return {
    on: vi.fn(() => () => {}),
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

describe("useRealtimeSync — chat-session task invalidation", () => {
  let qc: QueryClient;
  let ws: WSClient;
  let invalidate: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    vi.useFakeTimers();
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    ws = createMockWs();
    invalidate = vi.spyOn(qc, "invalidateQueries");
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: function Wrapper({ children }: { children: ReactNode }) {
        return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
      },
    });
    invalidate.mockClear();
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    vi.useRealTimers();
  });

  function emit(type: string, payload: unknown) {
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    if (!onAny) throw new Error("onAny handler was not registered");
    onAny({ type, payload } as never);
  }

  function seedIssues(...issueIds: string[]) {
    for (const issueId of issueIds) {
      qc.setQueryData(issueKeys.tasks(issueId), []);
      qc.setQueryData(issueKeys.usage(issueId), {});
    }
  }

  it.each([undefined, ""])("does not invalidate issue caches for a chat-session task with %s issue_id", (issueId) => {
    seedIssues("issue-1", "issue-2");
    invalidate.mockClear();

    emit("task:completed", {
      task_id: "chat-task",
      chat_session_id: "chat-1",
      ...(issueId === undefined ? {} : { issue_id: issueId }),
    });
    vi.advanceTimersByTime(100);

    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: issueKeys.tasksAll() });
    expect(invalidate).not.toHaveBeenCalledWith({ queryKey: issueKeys.usageAll() });
    expect(qc.getQueryState(issueKeys.tasks("issue-1"))?.isInvalidated).toBe(false);
    expect(qc.getQueryState(issueKeys.usage("issue-1"))?.isInvalidated).toBe(false);
    expect(qc.getQueryState(issueKeys.tasks("issue-2"))?.isInvalidated).toBe(false);
    expect(qc.getQueryState(issueKeys.usage("issue-2"))?.isInvalidated).toBe(false);
  });

  it("retains prefix invalidation for task events without issue_id, doc_id, or chat_session_id", () => {
    seedIssues("issue-1", "issue-2");
    invalidate.mockClear();

    emit("task:completed", { task_id: "legacy-task" });
    vi.advanceTimersByTime(100);

    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueKeys.tasksAll() });
    expect(invalidate).toHaveBeenCalledWith({ queryKey: issueKeys.usageAll() });
    expect(qc.getQueryState(issueKeys.tasks("issue-1"))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(issueKeys.usage("issue-2"))?.isInvalidated).toBe(true);
  });
});
