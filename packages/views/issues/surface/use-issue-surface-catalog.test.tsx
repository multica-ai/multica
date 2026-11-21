/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "@multica/core/issues/stores/surface-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type {
  Issue,
  IssueStatusEntry,
  IssueTableGroupsRequest,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { useIssueSurfaceController } from "./use-issue-surface-controller";

/**
 * The catalog is server state, so it can be late or absent. Two behaviours
 * depend on that and neither is expressible with a boolean "loaded" flag:
 *
 * - A CUSTOM status filter cannot be routed to a column until the catalog
 *   answers. Fetching zero branches meanwhile renders an empty board with no
 *   spinner; failing renders one permanently, with no way to retry.
 * - Each concrete status becomes a branch only after catalog resolution.
 */

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

const QA_ENTRY: IssueStatusEntry = {
  id: "s-qa",
  workspace_id: "ws-1",
  key: "qa",
  name: "QA",
  description: "",
  category: "started",
  color: "#ff0000",
  is_system: false,
  position: 1,
  archived_at: null,
  created_at: "",
  updated_at: "",
};

function deferred<T>() {
  let resolve!: (value: T) => void;
  let reject!: (error: Error) => void;
  const promise = new Promise<T>((res, rej) => {
    resolve = res;
    reject = rej;
  });
  return { promise, resolve, reject };
}

let groupRequests: IssueTableGroupsRequest[] = [];
let rowRequests: IssueTableRowsRequest[] = [];

function installApi(
  listIssueStatuses: () => Promise<unknown>,
  listProjects: () => Promise<unknown> = async () => ({ projects: [], total: 0 }),
  listIssues: () => Promise<unknown> = async () => ({ issues: [], total: 0 }),
) {
  groupRequests = [];
  rowRequests = [];
  setApiInstance({
    listIssueStatuses,
    listProjects,
    listIssueTableGroups: async (request: IssueTableGroupsRequest) => {
      groupRequests.push(request);
      return { query_fingerprint: "test", total: 0, groups: [], next_cursor: null };
    },
    listIssueTableRows: async (request: IssueTableRowsRequest) => {
      rowRequests.push(request);
      return {
        query_fingerprint: "test",
        group_key: request.group_key ?? null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      };
    },
    listIssueTableFacets: async () => ({ query_fingerprint: "test", total: 0, facets: [] }),
    listIssues,
    getWorkspaceWorkingAgents: async () => [],
    getChildIssueProgress: async () => ({ progress: [] }),
    getAgentTaskSnapshot: async () => ({ tasks: [] }),
  } as unknown as ApiClient);
}

function makeWrapper(qc: QueryClient, surfaceKey: string) {
  const store = getIssueSurfaceViewStore(surfaceKey);
  return {
    store,
    Wrapper: function Wrapper({ children }: { children: ReactNode }) {
      return (
        <QueryClientProvider client={qc}>
          <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
        </QueryClientProvider>
      );
    },
  };
}

let qc: QueryClient;
beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity } } });
  pruneIssueSurfaceViewStates([]);
});
afterEach(() => {
  cleanup();
  qc.clear();
  vi.restoreAllMocks();
});

describe("useIssueSurfaceController — custom status filter vs a late catalog", () => {
  it("stays loading while the catalog a custom filter depends on is in flight", async () => {
    const catalog = deferred<{ statuses: IssueStatusEntry[]; categories: never[]; total: number }>();
    installApi(() => catalog.promise);
    const { store, Wrapper } = makeWrapper(qc, "workspace:catalog-pending");
    act(() => store.getState().toggleStatusFilter("qa"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: Wrapper },
    );

    // The regression: the surface reported "loaded, zero results" and rendered
    // an empty board for the whole cold-load window.
    await waitFor(() => expect(result.current.isLoading).toBe(true));
    expect(result.current.isEmpty).toBe(false);
    // And nothing is fetched: with the filter unresolved the visible column set
    // falls back to ALL categories, so fetching would briefly show the
    // UNFILTERED board to someone who opened a saved `qa` view.
    expect(rowRequests).toEqual([]);

    await act(async () => {
      catalog.resolve({ statuses: [QA_ENTRY], categories: [], total: 1 });
      await catalog.promise;
    });

    // Once the catalog answers, the filter routes to the column `qa` behaves as.
    await waitFor(() => expect(result.current.visibleStatuses).toEqual(["qa"]));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
  });

  it("surfaces a retryable error instead of a silent empty board when the catalog fails", async () => {
    const listIssueStatuses = vi
      .fn()
      .mockRejectedValueOnce(new Error("catalog unavailable"))
      .mockResolvedValue({ statuses: [QA_ENTRY], categories: [], total: 1 });
    installApi(listIssueStatuses);
    const { store, Wrapper } = makeWrapper(qc, "workspace:catalog-failed");
    act(() => store.getState().toggleStatusFilter("qa"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() => expect(result.current.filterDependencyError?.kind).toBe("status"));
    // Not "no issues" — the surface cannot answer the question that was asked.
    expect(result.current.isEmpty).toBe(false);
    expect(rowRequests).toEqual([]);

    act(() => result.current.filterDependencyError!.retry());
    await waitFor(() => expect(result.current.filterDependencyError).toBeNull());
    await waitFor(() => expect(result.current.visibleStatuses).toEqual(["qa"]));
    await waitFor(() => expect(rowRequests.length).toBeGreaterThan(0));
    for (const request of rowRequests) {
      expect(request.query.filters.statuses).toEqual(["qa"]);
    }
  });
});

// The project-status filter is evaluated client-side on Gantt and
// the swimlane extra-children merge, against the project catalog. An
// unresolved catalog is not "no filter": returning UNFILTERED rows under an
// active chip is as wrong as blanking the surface, and a failed request would
// leave it that way for good.
describe("useIssueSurfaceController — project-status filter vs a late catalog", () => {
  function renderGantt(surfaceKey: string, projects: () => Promise<unknown>) {
    installApi(async () => ({ statuses: [], categories: [], total: 0 }), projects);
    const { store, Wrapper } = makeWrapper(qc, surfaceKey);
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleProjectStatusFilter("in_progress");
    });
    return renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["gantt"],
        }),
      { wrapper: Wrapper },
    );
  }

  it("stays loading while the project catalog is in flight", async () => {
    const projects = deferred<{ projects: never[]; total: number }>();
    const { result } = renderGantt("workspace:pstatus-pending", () => projects.promise);

    await waitFor(() => expect(result.current.isLoading).toBe(true));

    await act(async () => {
      projects.resolve({ projects: [], total: 0 });
      await projects.promise;
    });
    await waitFor(() => expect(result.current.isLoading).toBe(false));
  });

  it("surfaces a retryable error when the project catalog fails", async () => {
    const projects = deferred<never>();
    const { result } = renderGantt("workspace:pstatus-failed", () => projects.promise);

    await act(async () => {
      projects.reject(new Error("projects unavailable"));
      await projects.promise.catch(() => {});
    });

    await waitFor(() => expect(result.current.filterDependencyError?.kind).toBe("status"));
    expect(result.current.isEmpty).toBe(false);
  });

  describe("on a project Gantt", () => {
    const row = (id: string, projectId: string) =>
      ({
        id,
        workspace_id: "ws-1",
        number: 1,
        identifier: id,
        title: id,
        description: null,
        status: "todo",
        priority: "none",
        assignee_type: null,
        assignee_id: null,
        creator_type: "member",
        creator_id: "user-1",
        parent_issue_id: null,
        project_id: projectId,
        position: 1,
        stage: null,
        start_date: "2026-01-01",
        due_date: "2026-01-05",
        metadata: {},
        properties: {},
        created_at: "2026-01-01T00:00:00Z",
        updated_at: "2026-01-01T00:00:00Z",
      }) as Issue;
    const ROWS = [row("active-row", "p-active"), row("paused-row", "p-paused")];
    const PROJECTS = {
      projects: [
        { id: "p-active", status: "in_progress" },
        { id: "p-paused", status: "paused" },
      ],
      total: 2,
    };

    function renderProjectGantt(surfaceKey: string, listProjects: () => Promise<unknown>) {
      installApi(
        async () => ({ statuses: [], categories: [], total: 0 }),
        listProjects,
        async () => ({ issues: ROWS, total: ROWS.length }),
      );
      const { store, Wrapper } = makeWrapper(qc, surfaceKey);
      act(() => {
        store.getState().setViewMode("gantt");
        store.getState().toggleProjectStatusFilter("in_progress");
      });
      return renderHook(
        () =>
          useIssueSurfaceController({
            scope: { type: "project", projectId: "p-active" },
            modes: ["gantt"],
          }),
        { wrapper: Wrapper },
      );
    }

    it("draws only matching rows after a retry recovers the project list", async () => {
      const listProjects = vi
        .fn()
        .mockRejectedValueOnce(new Error("projects unavailable"))
        .mockResolvedValue(PROJECTS);
      const { result } = renderProjectGantt("project:pstatus-retry", listProjects);

      await waitFor(() => expect(result.current.filterDependencyError?.kind).toBe("status"));
      expect(result.current.ganttIssues).toHaveLength(ROWS.length);

      act(() => result.current.filterDependencyError!.retry());
      await waitFor(() => expect(result.current.filterDependencyError).toBeNull());
      await waitFor(() =>
        expect(result.current.filteredGanttIssues.map((issue) => issue.id)).toEqual(["active-row"]),
      );
      expect(result.current.isLoading).toBe(false);
    });

    it("keeps filtering by the last project list when a refetch fails", async () => {
      const listProjects = vi
        .fn()
        .mockResolvedValueOnce(PROJECTS)
        .mockRejectedValue(new Error("refetch failed"));
      const { result } = renderProjectGantt("project:pstatus-stale", listProjects);
      await waitFor(() =>
        expect(result.current.filteredGanttIssues.map((issue) => issue.id)).toEqual(["active-row"]),
      );

      await act(async () => {
        await qc.refetchQueries({ queryKey: ["projects", "ws-1", "list"] }).catch(() => {});
        // Query observers notify on a timer; let the failure reach the hook.
        await new Promise((resolve) => setTimeout(resolve, 20));
      });

      expect(qc.getQueryState(["projects", "ws-1", "list"])?.status).toBe("error");
      expect(result.current.filterDependencyError).toBeNull();
      expect(result.current.filteredGanttIssues.map((issue) => issue.id)).toEqual(["active-row"]);
    });
  });

  it("does not hold the surface when no project-status filter is active", async () => {
    const projects = deferred<{ projects: never[]; total: number }>();
    installApi(
      async () => ({ statuses: [], categories: [], total: 0 }),
      () => projects.promise,
    );
    const { store, Wrapper } = makeWrapper(qc, "workspace:pstatus-inactive");
    act(() => store.getState().setViewMode("gantt"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["gantt"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.filterDependencyError).toBeNull();
  });
});

describe("useIssueSurfaceController — swimlane protocol", () => {
  it("uses the category axis with no custom statuses", async () => {
    installApi(async () => ({ statuses: [], categories: [], total: 0 }));
    const { store, Wrapper } = makeWrapper(qc, "workspace:swimlane-plain");
    act(() => store.getState().setViewMode("swimlane"));

    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["swimlane"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() => expect(groupRequests.length).toBeGreaterThan(0));
    // Every request uses lifecycle grouping, even for built-in-only catalogs.
    for (const request of groupRequests) {
      expect(request.group).toMatchObject({ kind: "compound", secondary: "status" });
    }
    for (const request of rowRequests) {
      expect(request.group).toMatchObject({ kind: "compound", secondary: "status" });
    }
  });

  it("keeps the category axis when a custom status exists", async () => {
    installApi(async () => ({ statuses: [QA_ENTRY], categories: [], total: 1 }));
    const { store, Wrapper } = makeWrapper(qc, "workspace:swimlane-custom");
    act(() => store.getState().setViewMode("swimlane"));

    renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["swimlane"],
        }),
      { wrapper: Wrapper },
    );

    await waitFor(() =>
      expect(
        groupRequests.some((request) =>
          request.group.kind === "compound" && request.group.secondary === "status",
        ),
      ).toBe(true),
    );
  });
});
