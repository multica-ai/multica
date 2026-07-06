/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Workspace } from "../types";
import { useDeleteWorkspace } from "./mutations";
import { workspaceKeys } from "./queries";

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

  const seedList = () => {
    qc.setQueryData<Workspace[]>(workspaceKeys.list(), [
      makeWorkspace("ws-1", "keep-me"),
      makeWorkspace("ws-2", "delete-me"),
    ]);
  };

  const cachedList = () =>
    qc.getQueryData<Workspace[]>(workspaceKeys.list()) ?? [];

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteWorkspace = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ deleteWorkspace } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
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
});
