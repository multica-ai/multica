/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import type { WSClient } from "../api/ws-client";
import { operationalControlKeys } from "../operational-controls/queries";
import { useRealtimeSync, type RealtimeSyncStores } from "./use-realtime-sync";

const workspaceId = "11111111-1111-4111-8111-111111111111";

vi.mock("../platform/workspace-storage", () => ({
  getCurrentWsId: () => workspaceId,
  getCurrentSlug: () => "test-ws",
  createWorkspaceAwareStorage: (adapter: unknown) => adapter,
  registerForWorkspaceRehydration: () => {},
}));

vi.mock("../paths", () => ({
  useHasOnboarded: () => true,
  resolvePostAuthDestination: () => "/",
}));

type Handler = (payload: unknown) => void;

function createMockWs() {
  const handlers = new Map<string, Handler>();
  const ws = {
    on: vi.fn((event: string, handler: Handler) => {
      handlers.set(event, handler);
      return () => handlers.delete(event);
    }),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
  return { handlers, ws };
}

function createStores(): RealtimeSyncStores {
  return {
    authStore: Object.assign(() => ({}), {
      getState: () => ({ user: { id: "user-1" } }),
      subscribe: () => () => {},
      setState: () => {},
      destroy: () => {},
    }),
  } as unknown as RealtimeSyncStores;
}

function createWrapper(queryClient: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>
    );
  };
}

describe("useRealtimeSync operational controls", () => {
  it("subscribes to the validated workspace-only invalidation event", () => {
    const queryClient = new QueryClient();
    const { handlers, ws } = createMockWs();
    const key = operationalControlKeys.policy(workspaceId, "agent-1");
    queryClient.setQueryData(key, { configured: false, rules: [] });
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(queryClient),
    });

    const handler = handlers.get("operational_controls:changed");
    expect(handler).toBeTypeOf("function");
    handler?.({ workspace_id: workspaceId });
    expect(queryClient.getQueryState(key)?.isInvalidated).toBe(true);

    queryClient.setQueryData(key, { configured: false, rules: [] });
    handler?.({ workspace_id: workspaceId, agent_id: "forbidden" });
    expect(queryClient.getQueryState(key)?.isInvalidated).toBe(false);
  });

  it("evicts protected cache entries immediately after a self role downgrade", () => {
    const queryClient = new QueryClient();
    const { handlers, ws } = createMockWs();
    const key = operationalControlKeys.policy(workspaceId, "agent-1");
    queryClient.setQueryData(key, { configured: true, rules: [] });
    renderHook(() => useRealtimeSync(ws, createStores()), {
      wrapper: createWrapper(queryClient),
    });

    handlers.get("member:updated")?.({
      member: { user_id: "user-1", role: "member" },
    });

    expect(queryClient.getQueryData(key)).toBeUndefined();
  });
});
