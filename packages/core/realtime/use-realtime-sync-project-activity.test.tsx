/**
 * @vitest-environment jsdom
 */
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { act, renderHook, waitFor } from "@testing-library/react";
import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { WSClient } from "../api/ws-client";
import { projectListOptions } from "../projects/queries";
import type { Project } from "../types";
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

function createRecordingWs() {
  const handlers: Record<string, (p: unknown) => void> = {};
  const ws = {
    on: vi.fn((event: string, handler: (p: unknown) => void) => {
      handlers[event] = handler;
      return () => {};
    }),
    onAny: vi.fn(() => () => {}),
    onReconnect: vi.fn(() => () => {}),
  } as unknown as WSClient;
  return { ws, handlers };
}

const stores = {
  authStore: Object.assign(() => ({}), {
    getState: () => ({ user: { id: "u1" } }),
    subscribe: () => () => {},
    setState: () => {},
    destroy: () => {},
  }),
} as unknown as RealtimeSyncStores;

const project = (last_activity_at: string) =>
  ({
    id: "project-1",
    workspace_id: "ws-1",
    title: "Launch",
    status: "in_progress",
    priority: "high",
    created_at: "2026-11-01T00:00:00Z",
    updated_at: "2026-11-01T00:00:00Z",
    last_activity_at,
  }) as Project;

describe("useRealtimeSync — project recent activity", () => {
  let qc: QueryClient;
  let listProjects: ReturnType<typeof vi.fn>;

  function mount() {
    const { ws, handlers } = createRecordingWs();
    const view = renderHook(
      () => {
        useRealtimeSync(ws, stores);
        return useQuery(projectListOptions("ws-1"));
      },
      {
        wrapper: ({ children }: { children: ReactNode }) => (
          <QueryClientProvider client={qc}>{children}</QueryClientProvider>
        ),
      },
    );
    return { ...view, handlers };
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
    listProjects = vi
      .fn()
      .mockResolvedValueOnce({ projects: [project("2026-11-10T00:00:00Z")], total: 1 })
      .mockResolvedValue({ projects: [project("2026-11-15T03:00:00Z")], total: 1 });
    setApiInstance({ listProjects } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.clearAllMocks();
  });

  it("refetches the project list once per burst of comments on its issues", async () => {
    const { result, handlers } = mount();
    await waitFor(() => expect(result.current.data?.[0]?.last_activity_at).toBe("2026-11-10T00:00:00Z"));

    act(() => {
      handlers["comment:created"]?.({ comment: { id: "c1", issue_id: "issue-1" } });
      handlers["comment:updated"]?.({ comment: { id: "c1", issue_id: "issue-1" } });
      handlers["comment:deleted"]?.({ issue_id: "issue-1" });
    });
    expect(listProjects).toHaveBeenCalledTimes(1);

    await waitFor(() => expect(result.current.data?.[0]?.last_activity_at).toBe("2026-11-15T03:00:00Z"), {
      timeout: 3000,
    });
    expect(listProjects).toHaveBeenCalledTimes(2);
  });

  it("refetches after a label, property or metadata change, which carry no issue snapshot", async () => {
    const { result, handlers } = mount();
    await waitFor(() => expect(result.current.data).toBeDefined());

    act(() => {
      handlers["issue_labels:changed"]?.({ issue_id: "issue-1", labels: [] });
      handlers["issue_properties:changed"]?.({ issue_id: "issue-1", properties: {} });
      handlers["issue_metadata:changed"]?.({ issue_id: "issue-1", metadata: {} });
    });
    await waitFor(() => expect(listProjects).toHaveBeenCalledTimes(2), { timeout: 3000 });
  });
});
