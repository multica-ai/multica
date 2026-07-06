/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { defaultStorage } from "../platform/storage";
import type { Workspace } from "../types";
import { useDeleteWorkspace } from "./mutations";
import { workspaceKeys, workspaceListOptions } from "./queries";
import { unmarkWorkspaceDeletePending } from "./pending-delete";

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

describe("useDeleteWorkspace", () => {
  let qc: QueryClient;
  let deleteWorkspace: ReturnType<typeof vi.fn<(id: string) => Promise<void>>>;
  let listWorkspaces: ReturnType<typeof vi.fn<() => Promise<Workspace[]>>>;

  const staleServerList = () => [
    makeWorkspace("ws-1", "keep-me"),
    makeWorkspace("ws-2", "delete-me"),
  ];

  const seedList = () => {
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), staleServerList());
  };

  const cachedList = () =>
    qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteWorkspace = vi.fn().mockResolvedValue(undefined);
    listWorkspaces = vi.fn().mockResolvedValue(staleServerList());
    setApiInstance({ deleteWorkspace, listWorkspaces } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    // The tombstone registry is module state; onSettled unmarks it in every
    // test flow, but keep a belt-and-braces reset so one failing test can't
    // poison the others.
    unmarkWorkspaceDeletePending("ws-2");
    localStorage.clear();
    vi.restoreAllMocks();
  });

  it("removes the workspace from the list cache while the DELETE is pending", async () => {
    seedList();
    // Hold the DELETE open so we can observe the pending window — the bug
    // was that during this window the list cache still contained the
    // workspace, so any consumer re-presented it as selectable/current.
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
      // Let onMutate (cancelQueries + optimistic removal) run.
      await Promise.resolve();
    });

    expect(deleteWorkspace).toHaveBeenCalledWith("ws-2");
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1"]);

    await act(async () => {
      resolveDelete();
      await mutationDone;
    });
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1"]);
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

  it("rolls the list back when the DELETE fails", async () => {
    seedList();
    deleteWorkspace.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(result.current.mutateAsync("ws-2")).rejects.toThrow("boom");
    });

    await waitFor(() => {
      expect(cachedList().map((w) => w.id)).toEqual(["ws-1", "ws-2"]);
    });
  });

  it("clears the deleted slug's workspace-scoped storage on success, using the pre-removal slug", async () => {
    seedList();
    // The realtime `workspace:deleted` handler reverse-looks-up the slug from
    // the list cache — which the optimistic removal has already emptied on
    // the initiating client — so the mutation itself must own this cleanup.
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

  it("does not clear workspace-scoped storage when the DELETE fails", async () => {
    seedList();
    deleteWorkspace.mockRejectedValue(new Error("boom"));
    defaultStorage.setItem("multica_issue_draft:delete-me", "draft");

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(result.current.mutateAsync("ws-2")).rejects.toThrow("boom");
    });

    expect(defaultStorage.getItem("multica_issue_draft:delete-me")).toBe("draft");
  });

  it("keeps the workspace out of the cache when a list refetch lands mid-pending", async () => {
    seedList();
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
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1"]);

    // Simulate a refetch during the pending window (realtime invalidation /
    // reconnect recovery / explicit fetchQuery) where the server has not
    // committed the delete yet and still returns the old row.
    await act(async () => {
      await qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 });
    });
    expect(listWorkspaces).toHaveBeenCalled();
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1"]);

    await act(async () => {
      resolveDelete();
      await mutationDone;
    });
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1"]);
  });

  it("lifts the tombstone after a failed DELETE settles, so refetches show the restored row", async () => {
    seedList();
    deleteWorkspace.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useDeleteWorkspace(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await expect(result.current.mutateAsync("ws-2")).rejects.toThrow("boom");
    });

    await act(async () => {
      await qc.fetchQuery({ ...workspaceListOptions(), staleTime: 0 });
    });
    expect(cachedList().map((w) => w.id)).toEqual(["ws-1", "ws-2"]);
  });
});
