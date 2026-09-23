/**
 * @vitest-environment jsdom
 */
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { GlobalAgent } from "../types";
import { workspaceKeys } from "../workspace/queries";
import {
  useDeleteGlobalAgent,
  useEnableGlobalAgent,
  useUpdateGlobalAgent,
} from "./mutations";
import { globalAgentKeys } from "./queries";

function globalAgent(overrides: Partial<GlobalAgent> = {}): GlobalAgent {
  return {
    id: "ga-1",
    owner_id: "user-1",
    name: "Reviewer",
    description: "",
    instructions: "",
    avatar_url: null,
    conversation_starters: [],
    links: [
      {
        workspace_id: "ws-1",
        workspace_name: "Acme",
        workspace_slug: "acme",
        agent_id: "agent-1",
        archived: false,
        runtime_bound: true,
      },
    ],
    created_at: "2026-09-01T00:00:00Z",
    updated_at: "2026-09-01T00:00:00Z",
    ...overrides,
  };
}

function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  // Seed caches so invalidation is observable as isInvalidated=true.
  for (const wsId of ["ws-1", "ws-2", "ws-3"]) {
    qc.setQueryData(workspaceKeys.agents(wsId), []);
  }
  qc.setQueryData(globalAgentKeys.list(), [globalAgent()]);
  const wrapper = ({ children }: { children: ReactNode }) => (
    <QueryClientProvider client={qc}>{children}</QueryClientProvider>
  );
  const invalidated = (wsId: string) =>
    qc.getQueryState(workspaceKeys.agents(wsId))?.isInvalidated === true;
  return { qc, wrapper, invalidated };
}

describe("global agent mutations", () => {
  afterEach(() => vi.restoreAllMocks());

  it("refreshes every linked workspace after an update, old and new links", async () => {
    const { wrapper, invalidated, qc } = setup();
    setApiInstance({
      updateGlobalAgent: vi.fn().mockResolvedValue(
        globalAgent({
          name: "Senior reviewer",
          links: [
            {
              workspace_id: "ws-2",
              workspace_name: "Beta",
              workspace_slug: "beta",
              agent_id: "agent-2",
              archived: false,
              runtime_bound: true,
            },
          ],
        }),
      ),
    } as unknown as ApiClient);

    const { result } = renderHook(() => useUpdateGlobalAgent(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({ id: "ga-1", name: "Senior reviewer" });
    });

    expect(invalidated("ws-1")).toBe(true);
    expect(invalidated("ws-2")).toBe(true);
    expect(invalidated("ws-3")).toBe(false);
    await waitFor(() =>
      expect(qc.getQueryState(globalAgentKeys.list())?.isInvalidated).toBe(true),
    );
  });

  it("refreshes linked workspaces after delete without touching others", async () => {
    const { wrapper, invalidated } = setup();
    const deleteGlobalAgent = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ deleteGlobalAgent } as unknown as ApiClient);

    const { result } = renderHook(() => useDeleteGlobalAgent(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync(globalAgent());
    });

    expect(deleteGlobalAgent).toHaveBeenCalledWith("ga-1");
    expect(invalidated("ws-1")).toBe(true);
    expect(invalidated("ws-2")).toBe(false);
  });

  it("refreshes only the target workspace after enabling", async () => {
    const { wrapper, invalidated } = setup();
    const enableGlobalAgentInWorkspace = vi
      .fn()
      .mockResolvedValue({ id: "agent-3", workspace_id: "ws-3" });
    setApiInstance({ enableGlobalAgentInWorkspace } as unknown as ApiClient);

    const { result } = renderHook(() => useEnableGlobalAgent(), { wrapper });
    await act(async () => {
      await result.current.mutateAsync({
        id: "ga-1",
        workspace_id: "ws-3",
        runtime_id: "rt-3",
      });
    });

    expect(enableGlobalAgentInWorkspace).toHaveBeenCalledWith("ga-1", {
      workspace_id: "ws-3",
      runtime_id: "rt-3",
    });
    expect(invalidated("ws-3")).toBe(true);
    expect(invalidated("ws-1")).toBe(false);
  });
});
