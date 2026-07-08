/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { issueKeys } from "@multica/core/issues/queries";
import {
  getIssueSurfaceViewStore,
  pruneIssueSurfaceViewStates,
} from "@multica/core/issues/stores/surface-view-store";
import { ViewStoreProvider } from "@multica/core/issues/stores/view-store-context";
import type {
  AgentTask,
  Issue,
  IssueStatus,
  ListIssuesParams,
  ListIssuesResponse,
} from "@multica/core/types";
import { useIssueSurfaceController } from "./use-issue-surface-controller";

function makeIssue(
  overrides: Partial<Issue> & Pick<Issue, "id" | "status">,
): Issue {
  return {
    workspace_id: "ws-1",
    number: 1,
    identifier: "MUL-1",
    title: overrides.id,
    description: null,
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: "p1",
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    metadata: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...overrides,
  };
}

const updateIssueMutate = vi.hoisted(() => vi.fn());
const batchUpdateMutateAsync = vi.hoisted(() => vi.fn());
const batchDeleteMutateAsync = vi.hoisted(() => vi.fn());
const openModal = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/issues/mutations", () => ({
  useUpdateIssue: () => ({ mutate: updateIssueMutate, isPending: false }),
  useBatchUpdateIssues: () => ({
    mutateAsync: batchUpdateMutateAsync,
    isPending: false,
  }),
  useBatchDeleteIssues: () => ({
    mutateAsync: batchDeleteMutateAsync,
    isPending: false,
  }),
}));

vi.mock("@multica/core/modals", () => ({
  useModalStore: {
    getState: () => ({ open: openModal }),
  },
}));

vi.mock("../../i18n", () => ({
  useT: () => ({ t: () => "translated" }),
}));

function makeWrapper(qc: QueryClient, surfaceKey = "project:p1") {
  const store = getIssueSurfaceViewStore(surfaceKey);
  return function Wrapper({ children }: { children: ReactNode }) {
    return (
      <QueryClientProvider client={qc}>
        <ViewStoreProvider store={store}>{children}</ViewStoreProvider>
      </QueryClientProvider>
    );
  };
}

function never<T>() {
  return new Promise<T>(() => {});
}

describe("useIssueSurfaceController", () => {
  let qc: QueryClient;
  let listIssues: ReturnType<
    typeof vi.fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listIssues = vi.fn(() => never<ListIssuesResponse>());
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() => never<AgentTask[]>()),
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);
    pruneIssueSurfaceViewStates([]);
    updateIssueMutate.mockClear();
    openModal.mockClear();
    batchUpdateMutateAsync.mockResolvedValue(undefined);
    batchDeleteMutateAsync.mockResolvedValue(undefined);
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    vi.restoreAllMocks();
  });

  it("derives the project scope key, API filter, and sorted myList cache key", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setSortBy("priority");
    store.getState().setSortDirection("desc");

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(listIssues).toHaveBeenCalled());

    const expectedSort = { sort_by: "priority", sort_direction: "desc" } as const;
    const expectedFilter = { project_id: "p1" };

    expect(result.current.scopeKey).toBe("project:p1");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.sort).toEqual(expectedSort);
    expect(
      qc.getQueryCache().find({
        queryKey: issueKeys.myListSorted(
          "ws-1",
          "project:p1",
          expectedFilter,
          expectedSort,
        ),
        exact: true,
      }),
    ).toBeDefined();
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({
        project_id: "p1",
        sort_by: "priority",
        sort_direction: "desc",
      }),
    );
  });

  it("uses the workspace issue list query for workspace scope", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(listIssues).toHaveBeenCalled());

    expect(result.current.scopeKey).toBe("workspace:all");
    expect(result.current.filter).toEqual({});
    expect(result.current.loadMoreScope).toBeUndefined();
    expect(result.current.loadMoreFilter).toBeUndefined();
    expect(
      qc.getQueryCache().find({
        queryKey: issueKeys.listSorted("ws-1", {
          sort_by: "position",
          sort_direction: undefined,
        }),
        exact: true,
      }),
    ).toBeDefined();
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ status: "backlog", limit: 50, offset: 0 }),
    );
  });

  it("maps my assigned scope to the existing personal issue query contract", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    await waitFor(() => expect(listIssues).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "user-1" };
    expect(result.current.scopeKey).toBe("my:user-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(
      qc.getQueryCache().find({
        queryKey: issueKeys.myListSorted(
          "ws-1",
          "assigned",
          expectedFilter,
          { sort_by: "position", sort_direction: undefined },
        ),
        exact: true,
      }),
    ).toBeDefined();
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ assignee_id: "user-1" }),
    );
  });

  it("keeps actor scopes keyed by actor while using the shared list query shape", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: {
            type: "actor",
            actorType: "agent",
            actorId: "agent-1",
            relation: "assigned",
          },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "actor:agent:agent-1:assigned") },
    );

    await waitFor(() => expect(listIssues).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "agent-1" };
    expect(result.current.scopeKey).toBe("actor:agent:agent-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("actor:agent:agent-1:assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(
      qc.getQueryCache().find({
        queryKey: issueKeys.myListSorted(
          "ws-1",
          "actor:agent:agent-1:assigned",
          expectedFilter,
          { sort_by: "position", sort_direction: undefined },
        ),
        exact: true,
      }),
    ).toBeDefined();
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ assignee_id: "agent-1" }),
    );
  });

  it.each([
    {
      name: "project",
      surfaceKey: "project:p1",
      scope: { type: "project" as const, projectId: "p1" },
      expected: { project_id: "p1", status: "todo" },
    },
    {
      name: "my assigned",
      surfaceKey: "my:user-1:assigned",
      scope: { type: "my" as const, relation: "assigned" as const, userId: "user-1" },
      expected: {
        assignee_type: "member",
        assignee_id: "user-1",
        status: "todo",
      },
    },
    {
      name: "actor assigned",
      surfaceKey: "actor:agent:agent-1:assigned",
      scope: {
        type: "actor" as const,
        actorType: "agent" as const,
        actorId: "agent-1",
        relation: "assigned" as const,
      },
      expected: {
        assignee_type: "agent",
        assignee_id: "agent-1",
        status: "todo",
      },
    },
  ])("merges $name create defaults into the create modal payload", ({ scope, surfaceKey, expected }) => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope,
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, surfaceKey) },
    );

    act(() => {
      result.current.openCreateIssue({ status: "todo" });
    });

    expect(openModal).toHaveBeenCalledWith("create-issue", expected);
  });

  it("clears surface selection when the view mode changes within the same scope", async () => {
    const store = getIssueSurfaceViewStore("my:user-1:assigned");
    store.getState().setViewMode("list");

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    act(() => {
      result.current.selection.select(["issue-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["issue-1"]));

    act(() => {
      store.getState().setViewMode("board");
    });

    await waitFor(() => {
      expect(result.current.viewMode).toBe("board");
      expect(result.current.selection.selectedIds).toEqual(new Set());
    });
  });

  it("delegates movement through useUpdateIssue without rewriting the mutation path", () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );
    const onSettled = vi.fn();

    act(() => {
      result.current.moveIssue(
        "issue-1",
        { status: "in_progress", position: 42, project_id: "p2" },
        onSettled,
      );
    });

    expect(updateIssueMutate).toHaveBeenCalledWith(
      { id: "issue-1", status: "in_progress", position: 42, project_id: "p2" },
      expect.objectContaining({
        onError: expect.any(Function),
        onSettled: expect.any(Function),
      }),
    );

    const options = updateIssueMutate.mock.calls[0]?.[1] as
      | { onSettled?: () => void }
      | undefined;
    options?.onSettled?.();
    expect(onSettled).toHaveBeenCalled();
  });

  it("exposes surface actions and surface-local selection", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    act(() => {
      result.current.selection.select(["issue-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["issue-1"]));

    await act(async () => {
      await result.current.actions.batchUpdate(["issue-1"], { status: "done" });
      await result.current.actions.batchDelete(["issue-2"]);
    });

    expect(batchUpdateMutateAsync).toHaveBeenCalledWith({
      ids: ["issue-1"],
      updates: { status: "done" },
    });
    expect(batchDeleteMutateAsync).toHaveBeenCalledWith(["issue-2"]);
  });

  it("never reports isEmpty in gantt mode — an empty scheduled subset cannot prove the window is empty", async () => {
    // The gantt query returns only issues with a start/due date. A project
    // full of unscheduled issues comes back [] here, and the surface used to
    // conclude "no issues linked" and render the generic create-issue empty
    // state over GanttView's accurate "no scheduled issues" one.
    listIssues.mockResolvedValue({ issues: [], total: 0 });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.viewMode).toBe("gantt");
    // Falls through to GanttView, which renders its own scheduled-empty copy.
    expect(result.current.isEmpty).toBe(false);
  });

  it("reports isRefreshing while a view change revalidates behind the previous snapshot", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    listIssues.mockResolvedValue({ issues: [], total: 0 });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    // First load is loading, never refreshing — there is no previous
    // snapshot to show as a placeholder.
    expect(result.current.isLoading).toBe(true);
    expect(result.current.isRefreshing).toBe(false);
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Sort change: the key changes, the previous order stays rendered as a
    // placeholder while the new order fetches — refreshing, NOT loading.
    const resolvers: ((r: ListIssuesResponse) => void)[] = [];
    listIssues.mockImplementation(
      () => new Promise<ListIssuesResponse>((res) => resolvers.push(res)),
    );
    act(() => store.getState().setSortBy("priority"));

    await waitFor(() => expect(result.current.isRefreshing).toBe(true));
    expect(result.current.isLoading).toBe(false);

    // The revalidation lands — the indicator clears.
    await act(async () => {
      for (const resolve of resolvers) resolve({ issues: [], total: 0 });
    });
    await waitFor(() => expect(result.current.isRefreshing).toBe(false));
  });

  it("still reports isEmpty for the full-window modes when the list is empty", async () => {
    listIssues.mockResolvedValue({ issues: [], total: 0 });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.isEmpty).toBe(true);
  });

  // --- cancelled visibility (MUL-4261) ---------------------------------
  // Cancelled issues are always fetched into the cache, but the surface hides
  // them unless the status filter explicitly selects "cancelled".

  function mockListByStatus(byStatus: Partial<Record<IssueStatus, Issue[]>>) {
    listIssues.mockImplementation((params?: ListIssuesParams) => {
      const status = params?.status as IssueStatus | undefined;
      const issues = (status && byStatus[status]) ?? [];
      return Promise.resolve({ issues, total: issues.length });
    });
  }

  it("always fetches the cancelled bucket even though it is hidden by default", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(listIssues).toHaveBeenCalled());

    // The fetch layer requests the cancelled status page like any other.
    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ status: "cancelled", limit: 50, offset: 0 }),
    );
    // …but with no status filter, cancelled is not a visible column.
    expect(result.current.visibleStatuses).not.toContain("cancelled");
  });

  it("keeps cancelled issues out of the default surface and visible statuses", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      cancelled: [makeIssue({ id: "cancelled-1", status: "cancelled" })],
    });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.visibleStatuses).not.toContain("cancelled");
    const surfaceIds = result.current.surfaceIssues.map((i) => i.id);
    expect(surfaceIds).toContain("todo-1");
    expect(surfaceIds).not.toContain("cancelled-1");
    expect(result.current.issues.map((i) => i.id)).not.toContain("cancelled-1");
  });

  it("reveals cancelled issues only when the status filter selects cancelled", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      cancelled: [makeIssue({ id: "cancelled-1", status: "cancelled" })],
    });

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().toggleStatusFilter("cancelled"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // Cancelled becomes the sole visible column, sorted last in ALL_STATUSES.
    expect(result.current.visibleStatuses).toEqual(["cancelled"]);
    expect(result.current.issues.map((i) => i.id)).toEqual(["cancelled-1"]);
    expect(result.current.surfaceIssues.map((i) => i.id)).toContain(
      "cancelled-1",
    );
    // cancelled is never offered as a hideable/persistent board column.
    expect(result.current.hiddenStatuses).not.toContain("cancelled");
  });
});
