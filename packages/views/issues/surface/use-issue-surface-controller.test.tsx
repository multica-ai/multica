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
    properties: {},
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

function makeRunningTask(id: string, agentId: string, issueId: string): AgentTask {
  return {
    id,
    agent_id: agentId,
    runtime_id: "runtime-1",
    issue_id: issueId,
    status: "running",
    priority: 0,
    dispatched_at: null,
    started_at: "2026-01-01T00:00:00Z",
    completed_at: null,
    result: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };
}

describe("useIssueSurfaceController", () => {
  let qc: QueryClient;
  let listIssues: ReturnType<
    typeof vi.fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>
  >;
  let getAgentTaskSnapshot: ReturnType<
    typeof vi.fn<() => Promise<AgentTask[]>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listIssues = vi.fn(() => never<ListIssuesResponse>());
    getAgentTaskSnapshot = vi.fn(() => never<AgentTask[]>());
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot,
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

  // --- cancelled as a default status (MUL-4290) ------------------------
  // Cancelled is a first-class default lifecycle status: fetched into the
  // cache, surfaced by default, narrowed (not unlocked) by the status filter,
  // and hideable like any other status.

  function mockListByStatus(byStatus: Partial<Record<IssueStatus, Issue[]>>) {
    listIssues.mockImplementation((params?: ListIssuesParams) => {
      const status = params?.status as IssueStatus | undefined;
      const issues = (status && byStatus[status]) ?? [];
      return Promise.resolve({ issues, total: issues.length });
    });
  }

  it("fetches and surfaces the cancelled bucket as a default status", async () => {
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
    // …and with no status filter it is a visible column, ordered last.
    expect(result.current.visibleStatuses).toContain("cancelled");
    expect(result.current.visibleStatuses.at(-1)).toBe("cancelled");
  });

  it("includes cancelled issues in the default surface and visible statuses", async () => {
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

    expect(result.current.visibleStatuses).toContain("cancelled");
    const surfaceIds = result.current.surfaceIssues.map((i) => i.id);
    expect(surfaceIds).toContain("todo-1");
    expect(surfaceIds).toContain("cancelled-1");
    expect(result.current.issues.map((i) => i.id)).toContain("cancelled-1");
  });

  it("narrows the visible set to the selected statuses, dropping cancelled when it is not selected", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      cancelled: [makeIssue({ id: "cancelled-1", status: "cancelled" })],
    });

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().toggleStatusFilter("todo"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // The filter narrows the rendered columns and their contents — cancelled
    // is a normal status the filter can exclude, not an unlockable bucket.
    expect(result.current.visibleStatuses).toEqual(["todo"]);
    expect(result.current.issues.map((i) => i.id)).toEqual(["todo-1"]);
    // cancelled participates in show/hide like the rest — hidden here because
    // the active filter excludes it.
    expect(result.current.hiddenStatuses).toContain("cancelled");
  });

  it("treats a cancelled-only filter like any other narrowing status filter", async () => {
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

    // Cancelled becomes the sole visible column and the surface narrows to it.
    expect(result.current.visibleStatuses).toEqual(["cancelled"]);
    expect(result.current.issues.map((i) => i.id)).toEqual(["cancelled-1"]);
    expect(result.current.surfaceIssues.map((i) => i.id)).toContain(
      "cancelled-1",
    );
  });

  // --- working-chip scope (MUL-4884) ------------------------------------
  // The header chip promises "N issues in progress" where N is the number of
  // rows clicking it leaves. That only holds if the count comes out of the
  // same filter pipeline the rows do. It used to be re-derived from the task
  // snapshot against the PRE-filter issue set, so any active filter made the
  // chip disagree with the list it was filtering.

  it("keeps the working scope identical to the rendered rows when the filter is on", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "todo-1", status: "todo" }),
        makeIssue({ id: "todo-2", status: "todo" }),
      ],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    // Running work on todo-1 and prog-1; todo-2 is idle.
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "todo-1"),
      makeRunningTask("t-2", "agent-2", "prog-1"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().toggleAgentRunningFilter());

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() =>
      expect(result.current.workingScopeIssues.length).toBe(2),
    );

    // The scope the chip counts agents within must be the rendered list
    // itself — that identity is what keeps the chip in step with the filter
    // (the chip's own number counts agents, not these rows).
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual(
      result.current.issues.map((i) => i.id),
    );
    expect(result.current.issues.map((i) => i.id).sort()).toEqual([
      "prog-1",
      "todo-1",
    ]);
  });

  it("predicts the post-click rows while the filter is still off", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "todo-1", status: "todo" }),
        makeIssue({ id: "todo-2", status: "todo" }),
      ],
    });
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "todo-1"),
    ]);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() =>
      expect(result.current.workingScopeIssues.length).toBe(1),
    );

    // Filter off: the list still shows both rows, but the chip already
    // reports the one row a click would leave.
    expect(result.current.issues).toHaveLength(2);
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual([
      "todo-1",
    ]);
  });

  it("narrows the working scope with the status filter, exactly like the list", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    // Both issues have running agents...
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "todo-1"),
      makeRunningTask("t-2", "agent-2", "prog-1"),
    ]);

    // ...but the user is only looking at `todo`.
    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().toggleStatusFilter("todo"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() =>
      expect(result.current.workingScopeIssues.length).toBe(1),
    );

    // The regression: the chip used to say 2 here (both issues have running
    // agents) while clicking it produced a single row.
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual([
      "todo-1",
    ]);
  });

  it("hides sub-issues from the working scope when the list hides them", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "parent-1", status: "todo" }),
        makeIssue({ id: "child-1", status: "todo", parent_issue_id: "parent-1" }),
      ],
    });
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "child-1"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().toggleShowSubIssues());

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    // The only running work sits on a sub-issue the display toggle hides, so
    // clicking the filter would leave zero rows — the chip must say 0, not 1.
    expect(result.current.workingScopeIssues).toEqual([]);
  });

  it("keeps issue-less chat/autopilot tasks out of the working scope", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
    });
    // issue_id "" is how the API models a chat/autopilot task — it must not
    // become a phantom row (it used to inflate the count by exactly 1).
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "todo-1"),
      makeRunningTask("t-2", "agent-2", ""),
      makeRunningTask("t-3", "agent-3", ""),
    ]);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() =>
      expect(result.current.workingScopeIssues.length).toBe(1),
    );

    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual([
      "todo-1",
    ]);
  });

  it("scopes swimlane to the cards it draws, not its statusless lane source", async () => {
    // SwimLaneView draws cards from `issues` (status filter applied) and uses
    // the statusless `swimlaneIssues` only for LANE DISCOVERY. Scoping the
    // chip to the statusless set counted rows the canvas never draws.
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "todo-1"),
      makeRunningTask("t-2", "agent-2", "prog-1"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("swimlane");
      store.getState().toggleStatusFilter("todo");
    });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    await waitFor(() =>
      expect(result.current.workingScopeIssues.length).toBe(1),
    );

    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual([
      "todo-1",
    ]);
    expect(result.current.issues.map((i) => i.id)).toEqual(["todo-1"]);
    // The statusless lane source still carries both — so a regression back to
    // it would make the assertion above fail rather than pass by accident.
    expect(result.current.swimlaneIssues.map((i) => i.id).sort()).toEqual([
      "prog-1",
      "todo-1",
    ]);
  });

  // --- gantt canvas scope ------------------------------------------------
  // The gantt canvas draws fewer rows than the shared filters leave: a row
  // needs a date, and done/cancelled hide unless `ganttShowCompleted` is on.
  // Those rules live in the surface (`ganttCanvasRows`) so the chip narrows
  // the same set the canvas draws.

  function mockGanttIssues(issues: Issue[]) {
    listIssues.mockImplementation((params?: ListIssuesParams) => {
      if (params?.scheduled === true) {
        return Promise.resolve({ issues, total: issues.length });
      }
      return Promise.resolve({ issues: [], total: 0 });
    });
  }

  const ganttFixture = [
    makeIssue({
      id: "gantt-open",
      status: "in_progress",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    makeIssue({
      id: "gantt-done",
      status: "done",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    // Scheduled server-side but momentarily dateless (e.g. a WS patch that
    // just cleared both dates) — the canvas cannot place it.
    makeIssue({ id: "gantt-undated", status: "in_progress" }),
  ];

  it("keeps rows the gantt canvas hides out of the working scope", async () => {
    mockGanttIssues(ganttFixture);
    // Every one of them has a running agent.
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "gantt-open"),
      makeRunningTask("t-2", "agent-2", "gantt-done"),
      makeRunningTask("t-3", "agent-3", "gantt-undated"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => store.getState().setViewMode("gantt"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.filteredGanttIssues.length).toBe(1),
    );

    // ganttShowCompleted defaults to false, so the done row and the undated
    // row are not drawn — the chip must not count them either.
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual([
      "gantt-open",
    ]);
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual(
      result.current.filteredGanttIssues.map((i) => i.id),
    );
  });

  it("widens the gantt working scope when show-completed is turned on", async () => {
    mockGanttIssues(ganttFixture);
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-1", "agent-1", "gantt-open"),
      makeRunningTask("t-2", "agent-2", "gantt-done"),
      makeRunningTask("t-3", "agent-3", "gantt-undated"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleGanttShowCompleted();
    });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.filteredGanttIssues.length).toBe(2),
    );

    // The done row is drawn now, so it counts. The undated one still cannot
    // be placed, so it still does not.
    expect(result.current.workingScopeIssues.map((i) => i.id).sort()).toEqual([
      "gantt-done",
      "gantt-open",
    ]);
    expect(result.current.workingScopeIssues.map((i) => i.id)).toEqual(
      result.current.filteredGanttIssues.map((i) => i.id),
    );
  });
});
