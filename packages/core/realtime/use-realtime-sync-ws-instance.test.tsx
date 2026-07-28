/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi, beforeEach } from "vitest";
import type { WSClient } from "../api/ws-client";
import type { Issue } from "../types";
import { issueKeys } from "../issues/queries";
import { propertyKeys } from "../properties/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => "ws-1",
  getCurrentSlug: () => "test-ws",
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

function createCapturingMockWs(handlers: Map<string, (payload: unknown) => void>): WSClient {
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

  it("invalidates exactly once when a new ws instance appears after null gap", () => {
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
    // CEREBRO-PATCH(reconnect-wakeup-invalidate): FIR-1677 adds the inbox
    // wakeup-list key to the reconnect sweep. After cherry-pick 1485f43df added
    // squads + 6 per-issue caches, the count is now:
    // Custom property catalogs add one workspace-scoped cache, so the
    // reconnect sweep now covers 24 query families.
    expect(invalidateSpy).toHaveBeenCalledTimes(24);
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

  it("invalidates chat, pins, labels, properties, and invitations queries on ws instance change", () => {
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
    expect(calls).toContainEqual(["properties", "ws-1"]);
    expect(calls).toContainEqual(["workspaces", "ws-1", "invitations"]);
  });

  it("applies issue property events and invalidates catalog usage counts", () => {
    const handlers = new Map<string, (payload: unknown) => void>();
    const ws = createCapturingMockWs(handlers);
    const issue: Issue = {
      id: "issue-1",
      workspace_id: "ws-1",
      number: 1,
      identifier: "MUL-1",
      kind: "issue",
      title: "Prioritize launch",
      description: null,
      status: "todo",
      priority: "high",
      assignee_type: null,
      assignee_id: null,
      creator_type: "member",
      creator_id: "member-1",
      parent_issue_id: null,
      project_id: null,
      position: 1,
      metadata: {},
      properties: {},
      start_date: null,
      due_date: null,
      created_at: "2026-07-17T00:00:00Z",
      updated_at: "2026-07-17T00:00:00Z",
    };
    qc.setQueryData(issueKeys.detail("ws-1", issue.id), issue);
    qc.setQueryData(propertyKeys.list("ws-1"), { properties: [], total: 0 });

    renderHook(() => useRealtimeSync(ws, stores), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      handlers.get("issue_properties:changed")?.({
        issue_id: issue.id,
        properties: { "property-business-value": 125000 },
      });
    });

    expect(qc.getQueryData<Issue>(issueKeys.detail("ws-1", issue.id))?.properties).toEqual({
      "property-business-value": 125000,
    });
    expect(qc.getQueryState(propertyKeys.list("ws-1"))?.isInvalidated).toBe(true);
  });
});
