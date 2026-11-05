/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import {
  onlineManager,
  QueryClient,
  QueryClientProvider,
  useQuery,
} from "@tanstack/react-query";
import type { ReactNode } from "react";
import type { ListProjectsResponse, Project } from "../types";
import { projectKeys } from "./queries";
import { useRecentContextStore } from "../chat/recent-context-store";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { setCurrentWorkspace } from "../platform/workspace-storage";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "../issues/stores/surface-view-store";
import { issueKeys } from "../issues/queries";
import { useDeleteProject, useUpdateProject } from "./mutations";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useDeleteProject", () => {
  let qc: QueryClient;
  let deleteProject: ReturnType<typeof vi.fn<() => Promise<void>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    deleteProject = vi.fn().mockResolvedValue(undefined);
    setApiInstance({ deleteProject } as unknown as ApiClient);
    setCurrentWorkspace("acme", "ws-1");
  });

  afterEach(() => {
    onlineManager.setOnline(true);
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    useRecentContextStore.setState({ byWorkspace: {} });
    setCurrentWorkspace(null, null);
    vi.restoreAllMocks();
  });

  const project = { id: "p1", title: "Project" } as Project;
  const other = { id: "p2", title: "Other" } as Project;
  const listKey = projectKeys.list("ws-1");
  const detailKey = projectKeys.detail("ws-1", "p1");

  function seed() {
    qc.setQueryData<ListProjectsResponse>(listKey, {
      projects: [project, other],
      total: 2,
    });
    qc.setQueryData(detailKey, project);
  }

  function expectRetained() {
    expect(qc.getQueryData(listKey)).toEqual({
      projects: [project, other],
      total: 2,
    });
    expect(qc.getQueryData(detailKey)).toEqual(project);
  }

  function deferred<T>() {
    let resolve!: (value: T) => void;
    let reject!: (error: unknown) => void;
    const promise = new Promise<T>((res, rej) => {
      resolve = res;
      reject = rej;
    });
    return { promise, resolve, reject };
  }

  it.each([
    ["403", Object.assign(new Error("Forbidden"), { status: 403 })],
    ["network error", new TypeError("Failed to fetch")],
  ])(
    "retains cached data and local view state on %s",
    async (_label, error) => {
      seed();
      const store = getIssueSurfaceViewStore("project:p1");
      store.getState().setViewMode("list");
      useRecentContextStore
        .getState()
        .recordVisit("ws-1", { type: "project", id: "p1" });
      deleteProject.mockRejectedValue(error);
      const { result } = renderHook(() => useDeleteProject(), {
        wrapper: createWrapper(qc),
      });

      await act(async () => {
        await expect(result.current.mutateAsync("p1")).rejects.toBe(error);
      });

      expectRetained();
      expect(store.getState().viewMode).toBe("list");
      expect(useRecentContextStore.getState().byWorkspace["ws-1"]).toEqual([
        expect.objectContaining({ id: "p1" }),
      ]);
    },
  );

  it("keeps an offline deletion visible until reconnection and server confirmation", async () => {
    seed();
    onlineManager.setOnline(false);
    const pending = deferred<void>();
    deleteProject.mockReturnValue(pending.promise);
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    act(() => result.current.mutate("p1"));
    await waitFor(() => expect(result.current.isPaused).toBe(true));
    expect(deleteProject).not.toHaveBeenCalled();
    expectRetained();

    act(() => onlineManager.setOnline(true));
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    expectRetained();
    await act(async () => pending.resolve());
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(qc.getQueryData(listKey)).toEqual({ projects: [other], total: 1 });
    expect(qc.getQueryState(detailKey)).toBeUndefined();
  });

  it.each(["success", "failure"])(
    "finishes cache handling after the caller unmounts: %s",
    async (outcome) => {
      seed();
      qc.setQueryData(projectKeys.detail("ws-1", "p2"), other);
      qc.setQueryData(projectKeys.list("ws-2"), {
        projects: [project],
        total: 1,
      });
      qc.setQueryData(projectKeys.detail("ws-2", "p1"), project);
      const pending = deferred<void>();
      deleteProject.mockReturnValue(pending.promise);
      const { result, unmount } = renderHook(() => useDeleteProject(), {
        wrapper: createWrapper(qc),
      });
      let mutation!: Promise<void>;
      act(() => {
        mutation = result.current.mutateAsync("p1");
      });
      const settled = mutation.catch(() => undefined);
      await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
      unmount();
      await act(async () => {
        if (outcome === "success") pending.resolve();
        else pending.reject(new Error("Forbidden"));
        await settled;
      });
      if (outcome === "success") {
        expect(qc.getQueryData(listKey)).toEqual({
          projects: [other],
          total: 1,
        });
        expect(qc.getQueryState(detailKey)).toBeUndefined();
      } else expectRetained();
      expect(qc.getQueryData(projectKeys.detail("ws-1", "p2"))).toEqual(other);
      expect(qc.getQueryData(projectKeys.list("ws-2"))).toEqual({
        projects: [project],
        total: 1,
      });
      expect(qc.getQueryData(projectKeys.detail("ws-2", "p1"))).toEqual(
        project,
      );
    },
  );

  it("does not roll back a concurrent successful deletion or newer project data", async () => {
    seed();
    const pending = deferred<void>();
    deleteProject.mockReturnValueOnce(pending.promise);
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    let first!: Promise<void>;
    act(() => {
      first = result.current.mutateAsync("p1");
    });
    const settled = first.catch(() => undefined);
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    await act(async () => {
      await result.current.mutateAsync("p2");
    });
    const updated = { ...project, title: "Updated remotely" };
    qc.setQueryData(listKey, { projects: [updated], total: 1 });
    qc.setQueryData(detailKey, updated);
    await act(async () => {
      pending.reject(new Error("Forbidden"));
      await settled;
    });
    expect(qc.getQueryData(listKey)).toEqual({ projects: [updated], total: 1 });
    expect(qc.getQueryData(detailKey)).toEqual(updated);
  });

  it("does not resurrect a project removed by realtime when DELETE fails", async () => {
    seed();
    const pending = deferred<void>();
    deleteProject.mockReturnValue(pending.promise);
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    let mutation!: Promise<void>;
    act(() => {
      mutation = result.current.mutateAsync("p1");
    });
    const settled = mutation.catch(() => undefined);
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    qc.setQueryData(listKey, { projects: [other], total: 1 });
    qc.removeQueries({ queryKey: detailKey });
    await act(async () => {
      pending.reject(new Error("Not found"));
      await settled;
    });
    expect(qc.getQueryData(listKey)).toEqual({ projects: [other], total: 1 });
    expect(qc.getQueryState(detailKey)).toBeUndefined();
  });

  it("does not decrement total twice if realtime has already removed the project", async () => {
    seed();
    const pending = deferred<void>();
    deleteProject.mockReturnValue(pending.promise);
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    let mutation!: Promise<void>;
    act(() => {
      mutation = result.current.mutateAsync("p1");
    });
    await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
    qc.setQueryData(listKey, { projects: [other], total: 1 });
    await act(async () => {
      pending.resolve();
      await mutation;
    });
    expect(qc.getQueryData(listKey)).toEqual({ projects: [other], total: 1 });
  });

  it.each(["loaded", "initial"])(
    "replaces a stale %s list read after successful deletion",
    async (cache) => {
      if (cache === "loaded") seed();
      const pending = deferred<void>();
      deleteProject.mockReturnValue(pending.promise);
      const deletion = renderHook(() => useDeleteProject(), {
        wrapper: createWrapper(qc),
      });
      let mutation!: Promise<void>;
      act(() => {
        mutation = deletion.result.current.mutateAsync("p1");
      });
      await waitFor(() => expect(deleteProject).toHaveBeenCalledOnce());
      // A focus/realtime refresh may start while DELETE is still pending.
      const stale = deferred<ListProjectsResponse>();
      const fresh = deferred<ListProjectsResponse>();
      const fetchList = vi
        .fn()
        .mockReturnValueOnce(stale.promise)
        .mockReturnValue(fresh.promise);
      const { result } = renderHook(
        () => useQuery({ queryKey: listKey, queryFn: fetchList, staleTime: 0 }),
        { wrapper: createWrapper(qc) },
      );
      await waitFor(() => expect(fetchList).toHaveBeenCalledOnce());
      await act(async () => {
        pending.resolve();
        await mutation;
      });
      expect(fetchList).toHaveBeenCalledTimes(2);
      await act(async () => {
        fresh.resolve({ projects: [other], total: 1 });
        stale.resolve({ projects: [project, other], total: 2 });
      });
      await waitFor(() =>
        expect(result.current.data).toEqual({ projects: [other], total: 1 }),
      );
    },
  );

  it("does not create an absent list on success", async () => {
    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });
    await act(async () => {
      await result.current.mutateAsync("p1");
    });
    expect(qc.getQueryState(listKey)).toBeUndefined();
  });

  it("clears the deleted project's issue surface view state", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    expect(store.getState().viewMode).toBe("list");

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("p1");
    });

    expect(deleteProject).toHaveBeenCalledWith("p1");
    expect(store.getState().viewMode).toBe("board");
  });

  // Regression: the issue-table invalidation once sat on the create
  // mutation, so a missed realtime event left a project-status-filtered
  // window showing the deleted project's issues (staleTime is Infinity).
  it("invalidates the issue table windows", async () => {
    const tableKey = [...issueKeys.tableAll("ws-1"), "window"];
    qc.setQueryData(tableKey, { rows: [] });

    const { result } = renderHook(() => useDeleteProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync("p1");
    });

    expect(qc.getQueryState(tableKey)?.isInvalidated).toBe(true);
  });
});

describe("useUpdateProject", () => {
  let qc: QueryClient;
  let updateProject: ReturnType<typeof vi.fn>;
  const tableKey = [...issueKeys.tableAll("ws-1"), "window"];

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    updateProject = vi.fn().mockResolvedValue({ id: "p1" });
    setApiInstance({ updateProject } as unknown as ApiClient);
    qc.setQueryData(tableKey, { rows: [] });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("invalidates the issue table windows when the status changes", async () => {
    const { result } = renderHook(() => useUpdateProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ id: "p1", status: "paused" });
    });

    expect(qc.getQueryState(tableKey)?.isInvalidated).toBe(true);
  });

  it("leaves the issue table windows alone when the status is untouched", async () => {
    const { result } = renderHook(() => useUpdateProject(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ id: "p1", title: "Renamed" });
    });

    expect(qc.getQueryState(tableKey)?.isInvalidated).toBe(false);
  });
});
