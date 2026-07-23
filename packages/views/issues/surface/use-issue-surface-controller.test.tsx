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
  AgentTask,
  Issue,
  IssueStatus,
  ListIssuesParams,
  ListIssuesResponse,
  WorkspaceWorkingAgent,
} from "@multica/core/types";
import { useIssueSurfaceController } from "./use-issue-surface-controller";
import { IssueTableExportIntegrityError } from "../components/table-view-model";
import { statusTableMethodsFromLegacy } from "./status-table-test-api";

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

function makeWorkingAgent(
  id: string,
  issueIDs: string[] = [],
  runningTaskCount = 1,
): WorkspaceWorkingAgent {
  return {
    id,
    name: id,
    avatar_url: null,
    running_task_count: runningTaskCount,
    issue_ids: issueIDs,
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
  let getWorkspaceWorkingAgents: ReturnType<
    typeof vi.fn<() => Promise<WorkspaceWorkingAgent[]>>
  >;
  let listIssueTableRows: ReturnType<typeof vi.fn>;
  let listIssueTableFacets: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listIssues = vi.fn(() => never<ListIssuesResponse>());
    getAgentTaskSnapshot = vi.fn(() => never<AgentTask[]>());
    getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([] satisfies WorkspaceWorkingAgent[]),
    );
    const tableMethods = statusTableMethodsFromLegacy(listIssues);
    listIssueTableRows = vi.fn(tableMethods.listIssueTableRows);
    listIssueTableFacets = vi.fn(tableMethods.listIssueTableFacets);
    setApiInstance({
      listIssues,
      ...tableMethods,
      listIssueTableRows,
      listIssueTableFacets,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot,
      getWorkspaceWorkingAgents,
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

  it("derives the project scope and canonical server query", async () => {
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

    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalled());

    const expectedSort = { sort_by: "priority", sort_direction: "desc" } as const;
    const expectedFilter = { project_id: "p1" };

    expect(result.current.scopeKey).toBe("project:p1");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.sort).toEqual(expectedSort);
    expect(result.current.tableQuerySpec).toEqual(
      expect.objectContaining({
        scope: { kind: "project", project_id: "p1" },
        sort: { field: "priority", direction: "desc" },
      }),
    );
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        query: expect.objectContaining({
          scope: { kind: "project", project_id: "p1" },
        }),
        group: { kind: "status" },
      }),
    );
  });

  it("uses the unified workspace query for workspace scope", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalled());

    expect(result.current.scopeKey).toBe("workspace:all");
    expect(result.current.filter).toEqual({});
    expect(result.current.loadMoreScope).toBeUndefined();
    expect(result.current.loadMoreFilter).toBeUndefined();
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "workspace",
    });
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        group_key: "status:backlog",
        page: { limit: 50, cursor: null },
      }),
    );
  });

  it("does not subscribe List to the legacy issue endpoint", async () => {
    const legacyListIssues = vi.fn(() => never<ListIssuesResponse>());
    const tableRows = vi.fn(async (request: any) => ({
      query_fingerprint: "test",
      group_key: request.group_key,
      parent_id: null,
      total: 0,
      rows: [],
      branch_total: 0,
      next_cursor: null,
    }));
    const tableFacets = vi.fn(async () => ({
      query_fingerprint: "test",
      total: 0,
      facets: [{ kind: "status" as const, values: [] }],
    }));
    setApiInstance({
      listIssues: legacyListIssues,
      listIssueTableRows: tableRows,
      listIssueTableFacets: tableFacets,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot,
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "workspace", actorKind: "all" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "workspace:all") },
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(tableRows).toHaveBeenCalled();
    expect(tableFacets).toHaveBeenCalled();
    expect(legacyListIssues).not.toHaveBeenCalled();
  });

  it("maps my assigned scope to the unified personal query contract", async () => {
    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["board", "list", "swimlane"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "user-1" };
    expect(result.current.scopeKey).toBe("my:user-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "my",
      relation: "assigned",
    });
  });

  it("keeps actor scopes keyed by actor in the unified query shape", async () => {
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

    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalled());

    const expectedFilter = { assignee_id: "agent-1" };
    expect(result.current.scopeKey).toBe("actor:agent:agent-1:assigned");
    expect(result.current.filter).toEqual(expectedFilter);
    expect(result.current.loadMoreScope).toBe("actor:agent:agent-1:assigned");
    expect(result.current.loadMoreFilter).toEqual(expectedFilter);
    expect(result.current.tableQuerySpec.scope).toEqual({
      kind: "assignee",
      actor: { type: "agent", id: "agent-1" },
    });
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

    // Synchronous on purpose: the reset happens during render (not in an
    // effect), so no committed frame pairs the new view with the old
    // selection.
    expect(result.current.viewMode).toBe("board");
    expect(result.current.selection.selectedIds).toEqual(new Set());
  });

  it("delegates drag movement as a server-owned relative intent", () => {
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
        {
          status: "in_progress",
          position: 42,
          project_id: "p2",
          before_id: "issue-0",
          after_id: "issue-2",
        },
        onSettled,
      );
    });

    expect(updateIssueMutate).toHaveBeenCalledWith(
      {
        id: "issue-1",
        status: "in_progress",
        position: 42,
        project_id: "p2",
        move_intent: {
          before_id: "issue-0",
          after_id: "issue-2",
        },
      },
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

  it("debounces table search into the canonical server query without fetching the legacy flat window", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    listIssues.mockResolvedValue({ issues: [], total: 0 });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    listIssues.mockClear();

    act(() => result.current.setTableSearch("  Release train  "));

    expect(result.current.tableSearch).toBe("  Release train  ");
    expect(result.current.tableQuerySpec.search).toBeUndefined();
    await waitFor(() =>
      expect(result.current.tableQuerySpec.search).toBe("Release train"),
    );
    expect(listIssues).not.toHaveBeenCalled();
    expect(result.current.isEmpty).toBe(false);
  });

  it("loads only the Table facet whose filter submenu is active", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const listIssueTableFacets = vi.fn().mockResolvedValue({
      query_fingerprint: "sha256:facets",
      total: 0,
      facets: [{ kind: "status", values: [{ key: "todo", count: 2 }] }],
    });
    setApiInstance({
      listIssues,
      listIssueTableFacets,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    expect(listIssueTableFacets).not.toHaveBeenCalled();
    act(() => result.current.setActiveTableFacet({ kind: "status" }));
    await waitFor(() => expect(listIssueTableFacets).toHaveBeenCalledTimes(1));
    expect(listIssueTableFacets).toHaveBeenCalledWith(
      expect.objectContaining({
        facets: [{ kind: "status" }],
        include_total: false,
      }),
    );
    await waitFor(() =>
      expect(result.current.tableFacetCounts?.facets[0]?.kind).toBe("status"),
    );

    act(() => result.current.setActiveTableFacet(null));
    expect(result.current.tableFacetCounts).toBeUndefined();
  });

  it.each([
    {
      name: "Assignee Board",
      configure: (store: ReturnType<typeof getIssueSurfaceViewStore>) => {
        store.getState().setViewMode("board");
        store.getState().setGrouping("assignee");
      },
      properties: [],
    },
    {
      name: "Property Board",
      configure: (store: ReturnType<typeof getIssueSurfaceViewStore>) => {
        store.getState().setViewMode("board");
        store.getState().setGrouping("property:severity");
      },
      properties: [
        {
          id: "severity",
          workspace_id: "ws-1",
          name: "Severity",
          type: "select",
          config: { options: [] },
          position: 0,
          archived: false,
          created_at: "2026-01-01T00:00:00Z",
          updated_at: "2026-01-01T00:00:00Z",
        },
      ],
    },
    {
      name: "Swimlane",
      configure: (store: ReturnType<typeof getIssueSurfaceViewStore>) => {
        store.getState().setViewMode("swimlane");
      },
      properties: [],
    },
  ])(
    "loads exact filter facets on demand for the server-paged $name",
    async ({ configure, properties }) => {
      const store = getIssueSurfaceViewStore("project:p1");
      configure(store);
      const listIssueTableFacets = vi.fn().mockResolvedValue({
        query_fingerprint: "sha256:group-facets",
        total: 0,
        facets: [{ kind: "priority", values: [{ key: "high", count: 37 }] }],
      });
      const tableMethods = statusTableMethodsFromLegacy(listIssues);
      setApiInstance({
        listIssues,
        ...tableMethods,
        listIssueTableFacets,
        listGroupedIssues: vi.fn(() => never()),
        listProjects: vi.fn(() => never()),
        listProperties: vi.fn(() =>
          Promise.resolve({ properties, total: properties.length }),
        ),
        getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
        getChildIssueProgress: vi.fn(() => Promise.resolve([])),
      } as unknown as ApiClient);

      const { result } = renderHook(
        () =>
          useIssueSurfaceController({
            scope: { type: "project", projectId: "p1" },
            modes: ["board", "list", "swimlane"],
          }),
        { wrapper: makeWrapper(qc, "project:p1") },
      );

      expect(result.current.facetCountsExact).toBe(false);
      expect(listIssueTableFacets).not.toHaveBeenCalled();

      act(() => result.current.setActiveTableFacet({ kind: "priority" }));

      await waitFor(() => expect(listIssueTableFacets).toHaveBeenCalledTimes(1));
      expect(listIssueTableFacets).toHaveBeenCalledWith(
        expect.objectContaining({
          facets: [{ kind: "priority" }],
          include_total: false,
        }),
      );
      await waitFor(() =>
        expect(result.current.tableFacetCounts?.facets).toEqual([
          { kind: "priority", values: [{ key: "high", count: 37 }] },
        ]),
      );

      act(() => result.current.setActiveTableFacet(null));
      expect(result.current.tableFacetCounts).toBeUndefined();
    },
  );

  it("fails Table export closed when schema fallback would truncate the CSV", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const listIssueTableRows = vi.fn(() =>
      Promise.resolve({
        query_fingerprint: "",
        group_key: null,
        parent_id: null,
        total: 0,
        rows: [],
        branch_total: 0,
        next_cursor: null,
      }),
    );
    setApiInstance({
      listIssues,
      listIssueTableRows,
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await expect(result.current.exportTableIssues()).rejects.toBeInstanceOf(
      IssueTableExportIntegrityError,
    );
  });

  it("exports every stable cursor page exactly once", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    const first = makeIssue({ id: "issue-1", status: "todo" });
    const second = makeIssue({ id: "issue-2", status: "done" });
    const listIssueTableRows = vi
      .fn()
      .mockResolvedValueOnce({
        query_fingerprint: "sha256:export",
        group_key: null,
        parent_id: null,
        total: 2,
        rows: [{ issue: first, direct_child_count: 0 }],
        branch_total: 2,
        next_cursor: "cursor-2",
      })
      .mockResolvedValueOnce({
        query_fingerprint: "sha256:export",
        group_key: null,
        parent_id: null,
        total: 0,
        rows: [{ issue: second, direct_child_count: 0 }],
        branch_total: 1,
        next_cursor: null,
      });
    setApiInstance({
      listIssues,
      listIssueTableRows,
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await expect(result.current.exportTableIssues()).resolves.toEqual([
      first,
      second,
    ]);
    expect(listIssueTableRows).toHaveBeenNthCalledWith(
      2,
      expect.objectContaining({ page: { limit: 100, cursor: "cursor-2" } }),
    );
  });

  it("sends workspace running-task issue ids through the Table filter", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    store.getState().toggleAgentRunningFilter();
    listIssues.mockResolvedValue({ issues: [], total: 0 });
    const getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([
        {
          id: "agent-1",
          name: "Agent 1",
          avatar_url: null,
          running_task_count: 1,
          issue_ids: ["issue-running"],
        },
        {
          id: "agent-2",
          name: "Agent 2",
          avatar_url: null,
          running_task_count: 2,
          issue_ids: ["issue-running-2"],
        },
      ] satisfies WorkspaceWorkingAgent[]),
    );
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() =>
        Promise.resolve([
          { id: "task-1", issue_id: "issue-running", status: "running" },
        ] as unknown as AgentTask[]),
      ),
      getWorkspaceWorkingAgents,
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() =>
      expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual([
        "issue-running",
        "issue-running-2",
      ]),
    );
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("issue", undefined);
    expect(listIssues).not.toHaveBeenCalled();
  });

  it("uses the active My Issues relation for the Table working-agent filter", async () => {
    const store = getIssueSurfaceViewStore("my:user-1:assigned");
    store.getState().setViewMode("table");
    store.getState().toggleAgentRunningFilter();
    const getWorkspaceWorkingAgents = vi.fn(() =>
      Promise.resolve([] satisfies WorkspaceWorkingAgent[]),
    );
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getWorkspaceWorkingAgents,
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "my", relation: "assigned", userId: "user-1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "my:user-1:assigned") },
    );

    await waitFor(() =>
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith(
        "issue",
        "assigned",
      ),
    );
    expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual([]);
  });

  it.each(["board", "list", "swimlane"] as const)(
    "uses running tasks for the %s server query without reading the task snapshot",
    async (viewMode) => {
      const store = getIssueSurfaceViewStore("project:p1");
      store.getState().setViewMode(viewMode);
      store.getState().toggleAgentRunningFilter();
      getWorkspaceWorkingAgents.mockResolvedValue([
        makeWorkingAgent("agent-from-working-api", ["issue-from-working-api"]),
      ]);
      // Deliberately contradictory legacy data. It must neither be fetched nor
      // influence membership after the quick filter moved to working-agents.
      getAgentTaskSnapshot.mockResolvedValue([
        makeRunningTask("legacy-task", "legacy-agent", "legacy-issue"),
      ]);

      const { result } = renderHook(
        () =>
          useIssueSurfaceController({
            scope: { type: "project", projectId: "p1" },
            modes: ["board", "list", "swimlane"],
          }),
        { wrapper: makeWrapper(qc, "project:p1") },
      );

      await waitFor(() =>
        expect(
          result.current.tableQuerySpec.filters.working_issue_ids,
        ).toEqual(["issue-from-working-api"]),
      );
      expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
      expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("issue", undefined);
      expect(getAgentTaskSnapshot).not.toHaveBeenCalled();
    },
  );

  it("combines regular assignees with the independent running-task predicate", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    store.getState().toggleAssigneeFilter({ type: "agent", id: "agent-1" });
    store.getState().toggleAssigneeFilter({ type: "agent", id: "agent-2" });
    store.getState().toggleAssigneeFilter({ type: "member", id: "member-1" });
    store.getState().toggleNoAssignee();
    store.getState().toggleAgentRunningFilter();
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-2", ["member-assigned-running-issue"]),
      makeWorkingAgent("agent-3", ["unassigned-running-issue"]),
    ]);

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => {
      expect(result.current.tableQuerySpec.filters.assignees).toEqual([
        { type: "agent", id: "agent-1" },
        { type: "agent", id: "agent-2" },
        { type: "member", id: "member-1" },
      ]);
      expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual([
        "member-assigned-running-issue",
        "unassigned-running-issue",
      ]);
    });
    expect(result.current.tableQuerySpec.filters.include_no_assignee).toBe(true);
  });

  it("does not subscribe Table to the legacy offset window", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    // page 1 claims a small window (under the ceiling); by page 2 the real
    // window has grown far beyond it. The ceiling check must see the fresh
    // total — pagination itself already advances on it.
    listIssues.mockImplementation((params?: ListIssuesParams) =>
      Promise.resolve(
        (params?.offset ?? 0) === 0
          ? {
              issues: [
                makeIssue({ id: "i-1", status: "todo" }),
                makeIssue({ id: "i-2", status: "todo" }),
              ],
              total: 900,
            }
          : { issues: [makeIssue({ id: "i-3", status: "todo" })], total: 50_000 },
      ),
    );

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    expect(result.current.isLoading).toBe(false);
    expect(result.current.isEmpty).toBe(false);
    expect(listIssues).not.toHaveBeenCalled();
  });

  it("leaves Table empty/error ownership to the server-backed renderer", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("table");
    listIssues.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["table"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    // The legacy list endpoint is not part of Table rendering anymore.
    expect(listIssues).not.toHaveBeenCalled();
    expect(result.current.isEmpty).toBe(false);
  });

  it("clears surface selection when the membership window changes (filters, search)", async () => {
    const store = getIssueSurfaceViewStore("project:p1");
    store.getState().setViewMode("list");
    listIssues.mockResolvedValue({ issues: [], total: 0 });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["list"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    act(() => {
      result.current.selection.select(["issue-1"]);
    });
    expect(result.current.selection.selectedIds).toEqual(new Set(["issue-1"]));

    // Batch actions mutate raw selected ids while export/common-field
    // consumers intersect with visible rows — a selection surviving a
    // membership change would let the same "1 selected" mean different sets.
    // Asserted synchronously: the reset is render-phase, so not even one
    // frame commits the new membership with the old selection.
    act(() => {
      store.getState().toggleStatusFilter("todo");
    });

    expect(result.current.selection.selectedIds).toEqual(new Set());
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
  // rows clicking it leaves. The working-agents endpoint identifies both the
  // running agents and the issues referenced by those agents' running tasks.

  it("sends working issue ids to the server without reconstructing a local scope", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "todo-1", status: "todo" }),
        makeIssue({ id: "todo-2", status: "todo" }),
      ],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
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
    expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual([
      "todo-1",
      "prog-1",
    ]);
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(getAgentTaskSnapshot).not.toHaveBeenCalled();
    // Membership is cursor-paged and server-owned. A partial page cannot
    // produce the exact activity-chip scope, so it stays explicitly unknown.
    expect(result.current.workingScopeIssues).toBeUndefined();
  });

  it("keeps the activity-chip scope unknown before the server filter is enabled", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "todo-1", status: "todo" }),
        makeIssue({ id: "todo-2", status: "todo" }),
      ],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
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

    // Filter off: the list still shows both loaded rows, while the exact
    // post-click membership remains unknown until the backend predicate runs.
    expect(result.current.issues).toHaveLength(2);
    expect(result.current.workingScopeIssues).toBeUndefined();
  });

  it("combines the status and working predicates in the canonical server query", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
    ]);

    // ...but the user is only looking at `todo`.
    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().toggleStatusFilter("todo");
      store.getState().toggleAgentRunningFilter();
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

    expect(result.current.tableQuerySpec.filters.statuses).toEqual(["todo"]);
    expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual([
      "todo-1",
      "prog-1",
    ]);
    expect(result.current.tableQuerySpec.filters.assignees).toBeUndefined();
    expect(result.current.tableQuerySpec.filters.working_only).toBeUndefined();
    expect(result.current.workingScopeIssues).toBeUndefined();
  });

  it("sends the sub-issue display rule to the same server query", async () => {
    mockListByStatus({
      todo: [
        makeIssue({ id: "parent-1", status: "todo" }),
        makeIssue({ id: "child-1", status: "todo", parent_issue_id: "parent-1" }),
      ],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["parent-1"]),
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

    expect(result.current.tableQuerySpec.filters.include_sub_issues).toBe(false);
    expect(result.current.workingScopeIssues).toBeUndefined();
  });

  it("requests issue working agents so chat/autopilot work stays out of scope", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
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
    expect(result.current.workingScopeIssues).toEqual([]);
    expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith("issue", undefined);
    expect(getAgentTaskSnapshot).not.toHaveBeenCalled();
  });

  it("keeps swimlane chrome bounded while descriptors retain hidden-status counts", async () => {
    mockListByStatus({
      todo: [makeIssue({ id: "todo-1", status: "todo" })],
      in_progress: [makeIssue({ id: "prog-1", status: "in_progress" })],
    });
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-1", ["todo-1"]),
      makeWorkingAgent("agent-2", ["prog-1"]),
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
    // Like Table/List, the migrated Swimlane owns cursor branches and does
    // not materialize another complete working-only window for the chip.
    expect(result.current.workingScopeIssues).toBeUndefined();
    // Controller-only tests do not mount lane cells, so no row branch should
    // activate merely because its descriptor exists.
    expect(result.current.issues).toEqual([]);
    expect(result.current.swimlaneIssues).toEqual([]);
    expect(
      result.current.groupBranches?.descriptors
        .flatMap((lane) => lane.secondary_groups ?? [])
        .map((cell) => cell.value.kind === "status" ? cell.value.status : "")
        .sort(),
    ).toEqual(["in_progress", "todo"]);
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
      assignee_type: "agent",
      assignee_id: "agent-1",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    makeIssue({
      id: "gantt-done",
      status: "done",
      assignee_type: "agent",
      assignee_id: "agent-2",
      start_date: "2026-01-01",
      due_date: "2026-01-05",
    }),
    // Scheduled server-side but momentarily dateless (e.g. a WS patch that
    // just cleared both dates) — the canvas cannot place it.
    makeIssue({
      id: "gantt-undated",
      status: "in_progress",
      assignee_type: "agent",
      assignee_id: "agent-3",
    }),
  ];

  it("filters Gantt by running-task issue ids rather than issue assignees", async () => {
    mockGanttIssues(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue([
      // The editing agent deliberately differs from the issue assignee.
      makeWorkingAgent("agent-editor", ["gantt-open"]),
    ]);
    // Contradictory legacy membership must not affect the canvas.
    getAgentTaskSnapshot.mockResolvedValue([
      makeRunningTask("t-2", "agent-2", "gantt-done"),
      makeRunningTask("t-3", "agent-3", "gantt-undated"),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleAgentRunningFilter();
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
      expect(result.current.filteredGanttIssues.length).toBe(1),
    );

    // ganttShowCompleted defaults to false, so the done row and the undated
    // row are not drawn — the chip must not count them either.
    expect(result.current.workingScopeIssues?.map((i) => i.id)).toEqual([
      "gantt-open",
    ]);
    expect(result.current.workingScopeIssues?.map((i) => i.id)).toEqual(
      result.current.filteredGanttIssues.map((i) => i.id),
    );
    expect(getAgentTaskSnapshot).not.toHaveBeenCalled();
  });

  it.each([
    {
      name: "the working-agent API returns no agents",
      workingAgents: [] as WorkspaceWorkingAgent[],
      selectedAssigneeId: null,
    },
    {
      name: "the selected assignee excludes all running-task issues",
      workingAgents: [makeWorkingAgent("agent-1", ["gantt-open"])],
      selectedAssigneeId: "agent-2",
    },
  ])("keeps Gantt empty when $name", async ({
    workingAgents,
    selectedAssigneeId,
  }) => {
    mockGanttIssues(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue(workingAgents);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      if (selectedAssigneeId) {
        store.getState().toggleAssigneeFilter({
          type: "agent",
          id: selectedAssigneeId,
        });
      }
      store.getState().toggleAgentRunningFilter();
    });

    const { result } = renderHook(
      () =>
        useIssueSurfaceController({
          scope: { type: "project", projectId: "p1" },
          modes: ["board", "list", "swimlane", "gantt"],
        }),
      { wrapper: makeWrapper(qc, "project:p1") },
    );

    await waitFor(() => expect(result.current.ganttIssues).toHaveLength(3));
    await waitFor(() =>
      expect(getWorkspaceWorkingAgents).toHaveBeenCalledWith(
        "issue",
        undefined,
      ),
    );

    expect(result.current.tableQuerySpec.filters.working_issue_ids).toEqual(
      workingAgents.flatMap((agent) => agent.issue_ids),
    );
    expect(result.current.tableQuerySpec.filters.assignees).toEqual(
      selectedAssigneeId
        ? [{ type: "agent", id: selectedAssigneeId }]
        : undefined,
    );
    expect(result.current.filteredGanttIssues).toEqual([]);
    expect(result.current.workingScopeIssues).toEqual([]);
  });

  it("widens the gantt working scope when show-completed is turned on", async () => {
    mockGanttIssues(ganttFixture);
    getWorkspaceWorkingAgents.mockResolvedValue([
      makeWorkingAgent("agent-editor", ["gantt-open", "gantt-done"]),
    ]);

    const store = getIssueSurfaceViewStore("project:p1");
    act(() => {
      store.getState().setViewMode("gantt");
      store.getState().toggleAgentRunningFilter();
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
    expect(result.current.workingScopeIssues?.map((i) => i.id).sort()).toEqual([
      "gantt-done",
      "gantt-open",
    ]);
    expect(result.current.workingScopeIssues?.map((i) => i.id)).toEqual(
      result.current.filteredGanttIssues.map((i) => i.id),
    );
    expect(getAgentTaskSnapshot).not.toHaveBeenCalled();
  });
});
