/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider, QueryObserver, type InvalidateQueryFilters, type QueryKey } from "@tanstack/react-query";
import { cleanup, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach, afterEach } from "vitest";
import type { WSClient } from "../api/ws-client";
import { defaultStorage } from "../platform/storage";
import { issueKeys } from "../issues/queries";
import { chatKeys } from "../chat/queries";
import { runtimeKeys } from "../runtimes/queries";
import { workspaceWorkingAgentsKeys } from "../agents/queries";
import { workspaceKeys } from "../workspace/queries";
import { issueStatusKeys } from "../issue-statuses/queries";
import {
  markWorkspaceDeletePending,
  unmarkWorkspaceDeletePending,
} from "../workspace/pending-delete";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { IssueTableQuerySpec } from "../types";
import { getCurrentWsId } from "../platform/workspace-storage";
import { forgetLocalSearchIndex } from "../search-index/instance";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../search-index/instance", () => ({
  forgetLocalSearchIndex: vi.fn(async () => undefined),
}));

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: vi.fn(() => "ws-1"),
  getCurrentSlug: () => "test-ws",
  // Draft stores are now loaded transitively (storage-cleanup → register-all-drafts)
  // so their persist wiring must resolve against this mock.
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

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

function createWrapper(qc: QueryClient) {
  // Named function (not arrow) so react/display-name lint rule passes —
  // anonymous render-fn components break that rule even in test files.
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useRealtimeSync — ws instance change", () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;
  let invalidateSpy: ReturnType<typeof vi.spyOn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    stores = createStores();
    invalidateSpy = vi.spyOn(qc, "invalidateQueries");
  });

  it("skips invalidation on first non-null ws instance", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });

    // The main effect calls invalidateQueries for its own setup, but the
    // ws-instance-change effect should NOT have fired invalidation.
    // The only invalidateQueries calls should come from the main effect's
    // event handlers, not from the instance-change effect.
    // We verify by checking that no call was made with workspaceKeys.list()
    // pattern from the instance-change path (it logs a specific message).
    // Simpler: count calls — first mount with a ws should not trigger the
    // workspace-scoped bulk invalidation.
    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("does not invalidate when ws goes from instance to null", () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    invalidateSpy.mockClear();
    rerender({ ws: null });

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("invalidates exactly once when a new ws instance appears after null gap", async () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    // Simulate workspace switch: ws -> null -> new ws
    invalidateSpy.mockClear();
    rerender({ ws: null });
    expect(invalidateSpy).not.toHaveBeenCalled();

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    // Should have called invalidateQueries for all workspace-scoped keys
    // (16 workspace-scoped [incl. property definitions] + 6 per-issue
    // prefixes + the workspace working-agents projection + 6 per-chat
    // prefixes [incl. agent-messages monitoring] + 1 workspaceKeys.list() +
    // 1 cross-workspace inbox unread summary = 32 calls).
    //
    // Awaited rather than counted synchronously: the inbox unread summary
    // refresh cancels any in-flight request before invalidating (see
    // onInboxSummaryInvalidate), so that one lands after the synchronous ones.
    await waitFor(() => expect(invalidateSpy).toHaveBeenCalledTimes(32));
  });

  it("does not re-invalidate when rerendered with the same ws instance", () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    invalidateSpy.mockClear();
    // Rerender with same instance
    rerender({ ws: ws1 });

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("invalidates chat, pins, labels, and invitations queries on ws instance change", () => {
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map((call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey);
    expect(calls).toContainEqual(["chat", "ws-1"]);
    expect(calls).toContainEqual(["labels", "ws-1"]);
    expect(calls).toContainEqual(["workspaces", "ws-1", "invitations"]);
    // A catalog edit made while this client was disconnected is otherwise
    // invisible for the query's whole 5-minute staleTime.
    expect(calls).toContainEqual(issueStatusKeys.all("ws-1"));
  });

  it("invalidates agent projections when a daemon changes liveness", () => {
    vi.useFakeTimers();
    try {
      const ws = createMockWs();
      renderHook(() => useRealtimeSync(ws, stores), {
        wrapper: createWrapper(qc),
      });
      const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
      expect(onAny).toBeDefined();

      invalidateSpy.mockClear();
      onAny!({ type: "daemon:register", payload: {} } as never);
      vi.advanceTimersByTime(100);

      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: runtimeKeys.all("ws-1"),
      });
      expect(invalidateSpy).toHaveBeenCalledWith({
        queryKey: workspaceKeys.agents("ws-1"),
      });
    } finally {
      vi.useRealTimers();
    }
  });

  it("invalidates per-issue caches (no wsId in key) on ws instance change", () => {
    // These keys are not under the ["issues", wsId] prefix, so they need
    // their own invalidation on recovery — otherwise events missed while
    // disconnected leave them stale forever (staleTime: Infinity, #3953).
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map((call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey);
    expect(calls).toContainEqual(["issues", "timeline"]);
    expect(calls).toContainEqual(["issues", "reactions"]);
    expect(calls).toContainEqual(["issues", "subscribers"]);
    expect(calls).toContainEqual(["issues", "usage"]);
    expect(calls).toContainEqual(["issues", "attachments"]);
    expect(calls).toContainEqual(["issues", "tasks"]);
  });

  it("invalidates per-chat-session caches (no wsId in key) on ws instance change", () => {
    // These keys are not under the ["chat", wsId] prefix, so they need their
    // own recovery invalidation when reconnecting after missed chat/task events.
    const ws1 = createMockWs();
    const { rerender } = renderHook(
      ({ ws }) => useRealtimeSync(ws, stores),
      { initialProps: { ws: ws1 as WSClient | null }, wrapper: createWrapper(qc) },
    );

    invalidateSpy.mockClear();
    rerender({ ws: null });

    const ws2 = createMockWs();
    rerender({ ws: ws2 });

    const calls = invalidateSpy.mock.calls.map((call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey);
    expect(calls).toContainEqual(["chat", "messages"]);
    expect(calls).toContainEqual(["chat", "messages-page"]);
    expect(calls).toContainEqual(["chat", "pending-task"]);
    expect(calls).toContainEqual(["task-messages"]);
    expect(calls).toContainEqual(["chat", "agent-messages"]);
  });

  it("invalidates per-chat-session caches after an established ws reconnects", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const reconnect = vi.mocked(ws.onReconnect).mock.calls[0]?.[0];
    expect(reconnect).toBeDefined();

    invalidateSpy.mockClear();
    reconnect!();

    const calls = invalidateSpy.mock.calls.map((call: [{ queryKey?: unknown }, ...unknown[]]) => call[0].queryKey);
    expect(calls).toContainEqual(chatKeys.messagesAll());
    expect(calls).toContainEqual(chatKeys.messagesPageAll());
    expect(calls).toContainEqual(chatKeys.pendingTaskAll());
    expect(calls).toContainEqual(chatKeys.agentMessagesAll());
  });

  it("invalidates one issue attachment cache after detached channel media binds", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const attachmentChanged = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "issue_attachments:changed")?.[1];
    expect(attachmentChanged).toBeDefined();

    (attachmentChanged as (payload: unknown) => void)({ issue_id: "issue-1" });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: issueKeys.attachments("issue-1"),
    });
  });
  it("refetches the status catalog after an admin changes it elsewhere", async () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    onAny!({ type: "issue_status:changed", payload: { action: "created" } } as never);
    await new Promise((resolve) => setTimeout(resolve, 120));

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: issueStatusKeys.all("ws-1"),
    });
    const groupRefresh = invalidateSpy.mock.calls.find(([options]: [InvalidateQueryFilters?]) => options?.predicate);
    expect(groupRefresh?.[0]?.queryKey).toEqual([...issueKeys.tableAll("ws-1"), "groups"]);
    const predicate = groupRefresh![0]!.predicate!;
    expect(predicate({ queryKey: ["issues", "ws-1", "table-query", "groups", {}, { kind: "status" }] } as never)).toBe(true);
    expect(predicate({ queryKey: ["issues", "ws-1", "table-query", "groups", {}, { kind: "assignee" }] } as never)).toBe(false);
    // Deliberately NOT the issue caches. A row stores the status KEY; its name,
    // color and category are resolved from the catalog at render time, so no
    // cached issue field can go stale here. Dragging every board and list along
    // would turn one admin rename into a workspace-wide refetch storm on every
    // connected client. (MUL-6458)
    expect(invalidateSpy).not.toHaveBeenCalledWith({
      queryKey: issueKeys.all("ws-1"),
    });
  });

  it("ignores removed DingTalk group-route events", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]?.[0];
    expect(onAny).toBeDefined();

    onAny!({ type: "dingtalk_group_route:updated", payload: {} } as never);

    expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("invalidates the current workspace chat list when a channel creates a session", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const sessionCreated = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "chat:session_created")?.[1];
    expect(sessionCreated).toBeDefined();

    (sessionCreated as (payload: unknown) => void)({
      workspace_id: "ws-1",
      chat_session_id: "channel-session-1",
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: chatKeys.sessions("ws-1"),
    });
    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["chat", "ws-1", "agent-sessions"],
    });

		invalidateSpy.mockClear();
		(sessionCreated as (payload: unknown) => void)({
			workspace_id: "ws-2",
			chat_session_id: "other-workspace-session",
		});
		expect(invalidateSpy).not.toHaveBeenCalled();
  });

  it("invalidates the agent monitoring list when a session is renamed or archived", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const sessionUpdated = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "chat:session_updated")?.[1];
    expect(sessionUpdated).toBeDefined();

    invalidateSpy.mockClear();
    (sessionUpdated as (payload: unknown) => void)({
      chat_session_id: "session-1",
      status: "archived",
    });

    expect(invalidateSpy).toHaveBeenCalledWith({
      queryKey: ["chat", "ws-1", "agent-sessions"],
    });
  });
});

describe("useRealtimeSync — queued chat promotion", () => {
  it("refetches the transcript when a queued prompt starts running", () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const ws = createMockWs();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(qc),
    });
    const dispatch = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "task:dispatch")?.[1];
    expect(dispatch).toBeDefined();

    invalidate.mockClear();
    (dispatch as (payload: unknown) => void)({
      task_id: "task-follow-up",
      chat_session_id: "session-1",
    });

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: chatKeys.messages("session-1"),
    });
    expect(invalidate).toHaveBeenCalledWith({
      queryKey: chatKeys.messagesPage("session-1"),
    });
  });
});

describe("useRealtimeSync — Table server membership invalidation", () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    stores = createStores();
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    vi.mocked(getCurrentWsId).mockReturnValue("ws-1");
    vi.useRealTimers();
  });

  const spec: IssueTableQuerySpec = {
    scope: { kind: "workspace" },
    filters: {},
    sort: { field: "position", direction: "asc" },
  };
  const workingFacet = (wsId = "ws-1") => issueKeys.tableFacets(wsId, {
    query: spec, facets: [{ kind: "working_agents" }],
  });

  function mountRealtime() {
    vi.useFakeTimers();
    const ws = createMockWs();
    const hook = renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const onAny = vi.mocked(ws.onAny).mock.calls[0]![0];
    return { ...hook, emit: (type: string) => onAny({ type, payload: {} } as never) };
  }

  it("only invalidates working facets and queries whose membership depends on working state", async () => {
    const { emit } = mountRealtime();
    const untouched: QueryKey[] = [workingFacet("ws-2")];
    const affected: QueryKey[] = [workingFacet(), workspaceWorkingAgentsKeys.list("ws-1", "issue")];
    // Test both server membership forms, including an explicit empty id set,
    // across rows, descriptors and ordinary facets such as status counts.
    const filterCases: IssueTableQuerySpec["filters"][] = [{}, { working_only: false }, { working_only: true }, { working_issue_ids: [] }, { working_issue_ids: ["i1"] }];
    for (const filters of filterCases) {
      const query = { ...spec, filters };
      const keys = [
        issueKeys.tableRows("ws-1", query, { kind: "none" }, null, false, null),
        issueKeys.tableGroups("ws-1", query, { kind: "status" }),
        issueKeys.tableFacets("ws-1", { query, facets: [{ kind: "status" }] }),
      ];
      (filters.working_only || filters.working_issue_ids ? affected : untouched).push(...keys);
    }
    for (const key of [...untouched, ...affected]) qc.setQueryData(key, []);
    emit("task:completed");
    await vi.advanceTimersByTimeAsync(1_000);
    for (const key of affected) expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    for (const key of untouched) expect(qc.getQueryState(key)?.isInvalidated).toBe(false);
  });

  it("coalesces a continuous lifecycle stream and eventually clears the final completed run", async () => {
    const { emit } = mountRealtime();
    let count = 1;
    const queryFn = vi.fn(async () => count);
    qc.setQueryData(workingFacet(), count);
    const observer = new QueryObserver(qc, { queryKey: workingFacet(), queryFn });
    const unsubscribe = observer.subscribe(() => {});
    for (let i = 0; i < 20; i++) {
      emit(i % 2 ? "agent:updated" : "task:started");
      await vi.advanceTimersByTimeAsync(200);
    }
    expect(queryFn).toHaveBeenCalledTimes(4);
    count = 0;
    emit("task:completed");
    await vi.advanceTimersByTimeAsync(1_000);
    expect(queryFn).toHaveBeenCalledTimes(5);
    expect(observer.getCurrentResult().data).toBe(0);
    unsubscribe();
  });

  it.each(["facet", "projection"])("restarts an in-flight first %s request after completion instead of accepting its stale response", async (kind) => {
    const { emit } = mountRealtime();
    const queryKey = kind === "facet" ? workingFacet() : workspaceWorkingAgentsKeys.list("ws-1", "issue");
    let finishFirst!: (value: number) => void;
    const queryFn = vi.fn(async () => 0).mockImplementationOnce(() => new Promise<number>((resolve) => { finishFirst = resolve; }));
    const observer = new QueryObserver(qc, { queryKey, queryFn });
    const unsubscribe = observer.subscribe(() => {});
    emit("task:completed");
    await vi.advanceTimersByTimeAsync(1_000);
    expect(queryFn).toHaveBeenCalledTimes(2);
    expect(observer.getCurrentResult().data).toBe(0);
    finishFirst(1);
    await vi.advanceTimersByTimeAsync(0);
    expect(observer.getCurrentResult().data).toBe(0);
    unsubscribe();
  });

  it("lets slow refreshes finish and performs a trailing refresh for events received in flight", async () => {
    const { emit } = mountRealtime();
    let finishSlow!: (value: number) => void;
    const queryFn = vi.fn(async () => 0).mockImplementationOnce(() => new Promise<number>((resolve) => { finishSlow = resolve; }));
    qc.setQueryData(workingFacet(), 1);
    const observer = new QueryObserver(qc, { queryKey: workingFacet(), queryFn });
    const unsubscribe = observer.subscribe(() => {});
    emit("task:started");
    await vi.advanceTimersByTimeAsync(1_000);
    for (let i = 0; i < 10; i++) {
      emit("task:completed");
      await vi.advanceTimersByTimeAsync(200);
    }
    expect(queryFn).toHaveBeenCalledTimes(1);
    finishSlow(1);
    await vi.advanceTimersByTimeAsync(1_000);
    expect(queryFn).toHaveBeenCalledTimes(2);
    expect(observer.getCurrentResult().data).toBe(0);
    unsubscribe();
  });

  it("ignores progress/messages and discards a queued refresh on unmount", async () => {
    const { emit, unmount } = mountRealtime();
    qc.setQueryData(workingFacet(), []);
    qc.setQueryData(workspaceWorkingAgentsKeys.list("ws-1", "issue"), []);
    emit("task:progress");
    emit("task:message");
    await vi.advanceTimersByTimeAsync(2_000);
    expect(qc.getQueryState(workingFacet())?.isInvalidated).toBe(false);
    expect(qc.getQueryState(workspaceWorkingAgentsKeys.list("ws-1", "issue"))?.isInvalidated).toBe(false);
    emit("task:completed");
    unmount();
    await vi.advanceTimersByTimeAsync(1_000);
    expect(qc.getQueryState(workingFacet())?.isInvalidated).toBe(false);
  });

  it("keeps a queued refresh scoped to the workspace that received it", async () => {
    const { emit } = mountRealtime();
    qc.setQueryData(workingFacet(), []);
    qc.setQueryData(workingFacet("ws-2"), []);
    emit("task:completed");
    vi.mocked(getCurrentWsId).mockReturnValue("ws-2");
    await vi.advanceTimersByTimeAsync(1_000);
    expect(qc.getQueryState(workingFacet())?.isInvalidated).toBe(true);
    expect(qc.getQueryState(workingFacet("ws-2"))?.isInvalidated).toBe(false);
  });

  it("does not schedule a trailing refresh after unmounting during a slow request", async () => {
    const { emit, unmount } = mountRealtime();
    let finish!: (value: number) => void;
    const queryFn = vi.fn(() => new Promise<number>((resolve) => { finish = resolve; }));
    qc.setQueryData(workingFacet(), 1);
    const observer = new QueryObserver(qc, { queryKey: workingFacet(), queryFn });
    const unsubscribe = observer.subscribe(() => {});
    emit("task:started");
    await vi.advanceTimersByTimeAsync(1_000);
    emit("task:completed");
    unmount();
    finish(0);
    await vi.advanceTimersByTimeAsync(2_000);
    expect(queryFn).toHaveBeenCalledTimes(1);
    unsubscribe();
  });

  it("invalidates Table queries after a property definition changes", () => {
    const ws = createMockWs();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    const propertyUpdated = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "property:updated")?.[1];
    expect(propertyUpdated).toBeDefined();

    (propertyUpdated as (payload: unknown) => void)({});

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: issueKeys.tableAll("ws-1"),
    });
  });
});

describe("useRealtimeSync — workspace:deleted self-initiated suppression", () => {
  let qc: QueryClient;
  let stores: RealtimeSyncStores;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    stores = createStores();
  });

  afterEach(() => {
    unmarkWorkspaceDeletePending("ws-2");
    localStorage.clear();
    vi.mocked(forgetLocalSearchIndex).mockClear();
  });

  // getCurrentWsId is mocked to "ws-1" at module level, so deleting "ws-2"
  // never enters the relocate branch — these tests only exercise the
  // storage-cleanup path, which is the observable difference between a
  // handled and a suppressed event.
  const dispatchWorkspaceDeleted = (ws: WSClient, workspaceId: string) => {
    const call = vi
      .mocked(ws.on)
      .mock.calls.find(([event]) => event === "workspace:deleted");
    expect(call).toBeDefined();
    (call![1] as (p: unknown) => void)({ workspace_id: workspaceId });
  };

  it("ignores the event for a delete this client initiated", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    qc.setQueryData(workspaceKeys.list(), [{ id: "ws-2", slug: "delete-me" }]);
    defaultStorage.setItem("multica_issue_draft:delete-me", "draft");

    markWorkspaceDeletePending("ws-2");
    dispatchWorkspaceDeleted(ws, "ws-2");

    // useDeleteWorkspace.onSuccess owns cleanup for self-initiated deletes;
    // the handler must not have touched storage.
    expect(defaultStorage.getItem("multica_issue_draft:delete-me")).toBe("draft");
    expect(forgetLocalSearchIndex).not.toHaveBeenCalled();
  });

  it("still cleans up for a delete initiated elsewhere", () => {
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });
    qc.setQueryData(workspaceKeys.list(), [{ id: "ws-2", slug: "delete-me" }]);
    defaultStorage.setItem("multica_issue_draft:delete-me", "draft");

    dispatchWorkspaceDeleted(ws, "ws-2");

    expect(defaultStorage.getItem("multica_issue_draft:delete-me")).toBeNull();
    // Not the current workspace, but its local search copy must still go.
    expect(forgetLocalSearchIndex).toHaveBeenCalledWith("ws-2");
  });
});

describe("useRealtimeSync — member:removed", () => {
  afterEach(() => {
    vi.mocked(forgetLocalSearchIndex).mockClear();
  });

  const dispatchMemberRemoved = (ws: WSClient, payload: Record<string, string>) => {
    const call = vi.mocked(ws.on).mock.calls.find(([event]) => event === "member:removed");
    expect(call).toBeDefined();
    (call![1] as (p: unknown) => void)(payload);
  };

  it("destroys the local search copy when this user is removed", async () => {
    setApiInstance({ listWorkspaces: vi.fn().mockResolvedValue([]) } as unknown as ApiClient);
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    const ws = createMockWs();
    renderHook(() => useRealtimeSync(ws, createStores()), { wrapper: createWrapper(qc) });

    dispatchMemberRemoved(ws, { member_id: "m2", user_id: "someone-else", workspace_id: "ws-1" });
    expect(forgetLocalSearchIndex).not.toHaveBeenCalled();

    dispatchMemberRemoved(ws, { member_id: "m1", user_id: "u1", workspace_id: "ws-1" });
    expect(forgetLocalSearchIndex).toHaveBeenCalledWith("ws-1");
    // Let the relocate lookup settle before the test tears down.
    await waitFor(() => expect(qc.getQueryData(workspaceKeys.list())).toEqual([]));
  });
});
