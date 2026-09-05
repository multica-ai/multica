/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { defaultStorage } from "../platform/storage";
import type { Workspace, WorkspaceMcpServer } from "../types";
import {
  useCreateWorkspace,
  useDeleteWorkspace,
  useUpdateWorkspaceMcpServer,
} from "./mutations";
import { agentMcpServersOptions, workspaceKeys } from "./queries";
import {
  isWorkspaceDeletePending,
  unmarkWorkspaceDeletePending,
} from "./pending-delete";

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

const makeWorkspace = (id: string, slug: string): Workspace => ({
  id,
  name: slug,
  slug,
  description: null,
  context: null,
  settings: {},
  repos: [],
  issue_prefix: "MUL",
  avatar_url: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
});

describe("useCreateWorkspace", () => {
  let qc: QueryClient;
  let createWorkspace: ReturnType<
    typeof vi.fn<(data: { name: string; slug: string }) => Promise<Workspace>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    createWorkspace = vi.fn();
    setApiInstance({ createWorkspace } as unknown as ApiClient);
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      makeWorkspace("ws-1", "existing"),
    ]);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("seeds the successful response without invalidating the workspace list", async () => {
    const created = makeWorkspace("ws-2", "created");
    createWorkspace.mockResolvedValue(created);
    const { result } = renderHook(() => useCreateWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ name: "Created", slug: "created" });
    });

    expect(
      qc
        .getQueryData<Workspace[]>(workspaceKeys.list())
        ?.map((workspace) => workspace.id),
    ).toEqual(["ws-1", "ws-2"]);
    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(false);
  });

  it("invalidates the workspace list when create fails", async () => {
    createWorkspace.mockRejectedValue(new Error("response lost"));
    const { result } = renderHook(() => useCreateWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(
        result.current.mutateAsync({ name: "Created", slug: "created" }),
      ).rejects.toThrow("response lost");
    });

    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(true);
    expect(
      qc
        .getQueryData<Workspace[]>(workspaceKeys.list())
        ?.map((workspace) => workspace.id),
    ).toEqual(["ws-1"]);
  });
});

describe("useDeleteWorkspace", () => {
  let qc: QueryClient;
  let deleteWorkspace: ReturnType<typeof vi.fn<(id: string) => Promise<void>>>;
  let listWorkspaces: ReturnType<typeof vi.fn<() => Promise<Workspace[]>>>;

  const serverList = () => [
    makeWorkspace("ws-1", "keep-me"),
    makeWorkspace("ws-2", "delete-me"),
  ];

  const seedList = () => {
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), serverList());
  };

  const cachedList = () =>
    qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteWorkspace = vi.fn().mockResolvedValue(undefined);
    listWorkspaces = vi.fn().mockResolvedValue(serverList());
    setApiInstance({ deleteWorkspace, listWorkspaces } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    // The self-initiated marker is module state and is intentionally KEPT
    // after a successful delete (it suppresses the WS echo); reset it so
    // tests stay independent.
    unmarkWorkspaceDeletePending("ws-2");
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("leaves the list cache untouched while the DELETE is pending (no optimistic removal)", async () => {
    seedList();
    // Hold the DELETE open to observe the pending window. The flow awaits
    // the mutation with the dialog in a loading state, so the cache must
    // keep reflecting server truth: the workspace still exists.
    let resolveDelete!: () => void;
    deleteWorkspace.mockReturnValue(
      new Promise<void>((resolve) => {
        resolveDelete = resolve;
      }),
    );

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    let mutationDone: Promise<void>;
    await act(async () => {
      mutationDone = result.current.mutateAsync("ws-2");
      await Promise.resolve();
    });

    expect(deleteWorkspace).toHaveBeenCalledWith("ws-2");
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1", "ws-2"]);

    await act(async () => {
      resolveDelete();
      await mutationDone;
    });
  });

  it("invalidates the workspace list after a successful delete", async () => {
    seedList();
    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("ws-2");
    });

    expect(qc.getQueryState(workspaceKeys.list())?.isInvalidated).toBe(true);
  });

  it("clears the deleted slug's workspace-scoped storage on success", async () => {
    seedList();
    // The realtime `workspace:deleted` handler skips self-initiated deletes,
    // so the mutation owns this cleanup; the slug is captured from the list
    // cache before the mutation fires.
    defaultStorage.setItem("multica_issue_draft:delete-me", "draft");
    defaultStorage.setItem("multica_issue_draft:keep-me", "draft");

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("ws-2");
    });

    expect(defaultStorage.getItem("multica_issue_draft:delete-me")).toBeNull();
    expect(defaultStorage.getItem("multica_issue_draft:keep-me")).toBe("draft");
  });

  it("leaves storage and cache untouched when the DELETE fails", async () => {
    seedList();
    deleteWorkspace.mockRejectedValue(new Error("boom"));
    defaultStorage.setItem("multica_issue_draft:delete-me", "draft");

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(result.current.mutateAsync("ws-2")).rejects.toThrow("boom");
    });

    // No optimistic write happened, so there is nothing to roll back.
    expect(defaultStorage.getItem("multica_issue_draft:delete-me")).toBe("draft");
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1", "ws-2"]);
  });

  it("keeps the self-initiated marker after success and lifts it after failure", async () => {
    seedList();
    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    // Success: the id is gone for good; the kept marker suppresses the WS
    // echo of our own delete whenever it arrives.
    await act(async () => {
      await result.current.mutateAsync("ws-2");
    });
    expect(isWorkspaceDeletePending("ws-2")).toBe(true);

    // Failure: the workspace still exists, so a later external delete of
    // the same id must be handled by the realtime handler again.
    unmarkWorkspaceDeletePending("ws-2");
    deleteWorkspace.mockRejectedValue(new Error("boom"));
    await act(async () => {
      await expect(result.current.mutateAsync("ws-2")).rejects.toThrow("boom");
    });
    expect(isWorkspaceDeletePending("ws-2")).toBe(false);
  });
});

describe("useUpdateWorkspaceMcpServer", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it.each([
    {
      change: "renaming a server",
      update: { name: "renamed" },
      expected: { name: "renamed", transport: "http" },
    },
    {
      change: "replacing HTTP configuration with stdio",
      update: { config: { command: "mcp-server", args: [] } },
      expected: { name: "original", transport: "stdio" },
    },
  ])("refreshes active and invalidates inactive agent assignments after $change", async ({ update, expected }) => {
    const original: WorkspaceMcpServer = {
      id: "server-1",
      workspace_id: "ws-1",
      name: "original",
      transport: "http",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    let current = original;
    const updateWorkspaceMcpServer = vi.fn<ApiClient["updateWorkspaceMcpServer"]>()
      .mockImplementation(async () => {
        current = { ...original, ...expected };
        return current;
      });
    const listAgentMcpServers = vi.fn<ApiClient["listAgentMcpServers"]>()
      .mockImplementation(async () => [{ ...current, enabled: true }]);
    setApiInstance({ updateWorkspaceMcpServer, listAgentMcpServers } as unknown as ApiClient);

    const activeKey = agentMcpServersOptions("agent-1").queryKey;
    const inactiveKey = agentMcpServersOptions("agent-2").queryKey;
    qc.setQueryData(workspaceKeys.mcpServers("ws-1"), [original]);
    qc.setQueryData(activeKey, [{ ...original, enabled: true }]);
    qc.setQueryData(inactiveKey, [{ ...original, enabled: false }]);

    const { result } = renderHook(() => ({
      assignment: useQuery(agentMcpServersOptions("agent-1")),
      updateServer: useUpdateWorkspaceMcpServer("ws-1"),
    }), { wrapper: createWrapper(qc) });

    // Infinity staleTime reproduces a previously visited agent page whose
    // assignment otherwise never refreshes when the workspace library changes.
    expect(result.current.assignment.data).toEqual([{ ...original, enabled: true }]);
    expect(listAgentMcpServers).not.toHaveBeenCalled();

    await act(async () => {
      await result.current.updateServer.mutateAsync({ serverId: "server-1", ...update });
    });

    expect(updateWorkspaceMcpServer).toHaveBeenCalledWith("ws-1", "server-1", update);
    expect(qc.getQueryState(workspaceKeys.mcpServers("ws-1"))?.isInvalidated).toBe(true);
    expect(qc.getQueryState(inactiveKey)?.isInvalidated).toBe(true);
    await waitFor(() => {
      expect(result.current.assignment.data).toEqual([{ ...original, ...expected, enabled: true }]);
    });
    expect(listAgentMcpServers).toHaveBeenCalledWith("agent-1");
  });

  it("refetches authoritative assignments while preserving a failed update's rejection", async () => {
    const original: WorkspaceMcpServer = {
      id: "server-1",
      workspace_id: "ws-1",
      name: "original",
      transport: "http",
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
    };
    const responseLost = new Error("response lost after update committed");
    const authoritative = [{ ...original, name: "renamed", enabled: false }];
    const updateWorkspaceMcpServer = vi.fn<ApiClient["updateWorkspaceMcpServer"]>()
      .mockRejectedValue(responseLost);
    const listAgentMcpServers = vi.fn<ApiClient["listAgentMcpServers"]>()
      .mockResolvedValue(authoritative);
    setApiInstance({ updateWorkspaceMcpServer, listAgentMcpServers } as unknown as ApiClient);

    qc.setQueryData(workspaceKeys.mcpServers("ws-1"), [original]);
    qc.setQueryData(agentMcpServersOptions("agent-1").queryKey, [
      { ...original, enabled: true },
    ]);
    const { result } = renderHook(() => ({
      assignment: useQuery(agentMcpServersOptions("agent-1")),
      updateServer: useUpdateWorkspaceMcpServer("ws-1"),
    }), { wrapper: createWrapper(qc) });

    expect(listAgentMcpServers).not.toHaveBeenCalled();
    await act(async () => {
      await expect(result.current.updateServer.mutateAsync({
        serverId: "server-1",
        name: "renamed",
      })).rejects.toBe(responseLost);
    });

    expect(qc.getQueryState(workspaceKeys.mcpServers("ws-1"))?.isInvalidated).toBe(true);
    await waitFor(() => {
      expect(result.current.assignment.data).toEqual(authoritative);
      expect(result.current.updateServer.error).toBe(responseLost);
    });
    expect(listAgentMcpServers).toHaveBeenCalledWith("agent-1");
  });
});
