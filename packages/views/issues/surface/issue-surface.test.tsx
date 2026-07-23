/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import {
  act,
  cleanup,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { pruneIssueSurfaceViewStates } from "@multica/core/issues/stores/surface-view-store";
import type {
  AgentTask,
  Issue,
  IssueTableRowsRequest,
  ListIssuesParams,
  ListIssuesResponse,
} from "@multica/core/types";
import { IssueSurface } from "./issue-surface";

// Mutable so tests can simulate a workspace switch — the workspace layout
// does not remount its children on switch, so the surface must handle the
// wsId change itself.
const mockWsId = vi.hoisted(() => ({ current: "ws-1" }));
const mockTranslate = vi.hoisted(() => vi.fn(() => "translated"));
vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => mockWsId.current,
}));

// The list/board virtualize their rows via react-virtuoso; jsdom has no layout
// so the real Virtuoso renders nothing (and throws on its resize plumbing).
// Render items inline so these surface-level loading-semantics assertions still
// see the issues the virtualized list would show.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent, components }: any) => (
    <div data-testid="virtuoso-mock">
      {(data ?? []).map((item: any, i: number) => (
        <div key={i}>{itemContent(i, item)}</div>
      ))}
      {components?.Footer ? <components.Footer /> : null}
    </div>
  ),
}));

vi.mock("@tanstack/react-virtual", () => ({
  useVirtualizer: ({ count, estimateSize }: any) => ({
    getVirtualItems: () =>
      Array.from({ length: count }, (_, index) => ({
        index,
        start: index * estimateSize(),
        end: (index + 1) * estimateSize(),
      })),
    getTotalSize: () => count * estimateSize(),
  }),
}));

const mockAuthUser = { id: "user-1", email: "test@test.com", name: "Test User" };
vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (state: unknown) => unknown) => {
      const state = { user: mockAuthUser, isAuthenticated: true };
      return selector ? selector(state) : state;
    },
    { getState: () => ({ user: mockAuthUser, isAuthenticated: true }) },
  ),
  registerAuthStore: vi.fn(),
  createAuthStore: vi.fn(),
}));

vi.mock("../../i18n", () => ({
  // TableView also reads `i18n.language` for its date formatting.
  useT: () => ({ t: mockTranslate, i18n: { language: "en" } }),
  useTimeAgo: () => () => "now",
}));

vi.mock("../../navigation", () => ({
  AppLink: ({ children, href, ...props }: React.ComponentProps<"a">) => (
    <a href={href} {...props}>
      {children}
    </a>
  ),
  useNavigation: () => ({ push: vi.fn(), pathname: "/" }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/paths")>(
    "@multica/core/paths",
  );
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Test WS", slug: "test" }),
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

function makeIssue(id: string, title: string, projectId: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title,
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
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
  };
}

function never<T>() {
  return new Promise<T>(() => {});
}

function projectSurface(projectId: string) {
  return (
    <IssueSurface
      scope={{ type: "project", projectId }}
      modes={["list"]}
      renderHeader={() => null}
      renderLoading={() => <div data-testid="surface-loading" />}
      batchToolbar="never"
    />
  );
}

describe("IssueSurface — scope switch loading semantics", () => {
  let qc: QueryClient;

  beforeEach(() => {
    mockWsId.current = "ws-1";
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    // p1 answers immediately with one issue; p2 stays in flight forever so
    // the test can observe the in-between state after switching.
    const listIssues = vi.fn((params?: ListIssuesParams) => {
      if (params?.project_id === "p2") return never<ListIssuesResponse>();
      const issues =
        params?.status === "todo" ? [makeIssue("i1", "P1 issue", "p1")] : [];
      return Promise.resolve({ issues, total: issues.length });
    });
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() => never<AgentTask[]>()),
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);
    pruneIssueSurfaceViewStates([]);
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    vi.restoreAllMocks();
  });

  it("shows loading — not the previous project's issues — while the next project is fetching", async () => {
    // Regression: the list queries use `placeholderData: keepPreviousData` to
    // keep sort/filter changes flicker-free WITHIN one surface. Without a
    // scope-keyed remount, that placeholder leaks ACROSS surfaces: switching
    // pinned projects kept rendering project A's cards (isLoading=false, so
    // no skeleton either) until project B's response landed — the "click does
    // nothing, then it snaps" bug.
    const { rerender } = render(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );

    await screen.findByText("P1 issue");

    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p2")}</QueryClientProvider>,
    );

    // The switch must be honest: p2 has no data yet, so the surface is
    // loading — p1's cards must not impersonate p2.
    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();
    expect(screen.queryByText("P1 issue")).not.toBeInTheDocument();
  });

  it("shows a cached project instantly on switch-back (no loading flash)", async () => {
    const { rerender } = render(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );
    await screen.findByText("P1 issue");

    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p2")}</QueryClientProvider>,
    );
    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();

    // Back to p1: its cache is warm, so the list renders immediately from
    // cache — remounting must not degrade the instant-switch path.
    rerender(
      <QueryClientProvider client={qc}>{projectSurface("p1")}</QueryClientProvider>,
    );
    await waitFor(() =>
      expect(screen.getByText("P1 issue")).toBeInTheDocument(),
    );
    expect(screen.queryByTestId("surface-loading")).not.toBeInTheDocument();
  });

  it("shows loading on a workspace switch even though the scope key is identical", async () => {
    // The workspace layout does NOT remount children on switch, and two
    // workspaces share the same scope key (e.g. "workspace:all") — so the
    // remount key must include wsId, or workspace A's issues impersonate
    // workspace B's while B is still fetching.
    //
    // A fresh element per render — reusing one element reference would let
    // React bail out of re-rendering the subtree entirely, and the wsId
    // change would never propagate.
    const workspaceSurface = () => (
      <IssueSurface
        scope={{ type: "workspace" }}
        modes={["list"]}
        renderHeader={() => null}
        renderLoading={() => <div data-testid="surface-loading" />}
        batchToolbar="never"
      />
    );

    const listIssues = vi.fn((params?: ListIssuesParams) => {
      const issues =
        params?.status === "todo" ? [makeIssue("i1", "WS1 issue", "p1")] : [];
      return Promise.resolve({ issues, total: issues.length });
    });
    setApiInstance({
      listIssues,
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() => never<AgentTask[]>()),
      getChildIssueProgress: vi.fn(() => never()),
    } as unknown as ApiClient);

    const { rerender } = render(
      <QueryClientProvider client={qc}>{workspaceSurface()}</QueryClientProvider>,
    );
    await screen.findByText("WS1 issue");

    // Switch workspace: same scope, new wsId, and the new workspace's
    // fetches hang so the in-between state is observable.
    listIssues.mockImplementation(() => never<ListIssuesResponse>());
    mockWsId.current = "ws-2";
    rerender(
      <QueryClientProvider client={qc}>{workspaceSurface()}</QueryClientProvider>,
    );

    expect(screen.getByTestId("surface-loading")).toBeInTheDocument();
    expect(screen.queryByText("WS1 issue")).not.toBeInTheDocument();
  });
});

describe("IssueSurface — table pagination ownership", () => {
  let qc: QueryClient;
  let listIssues: ReturnType<
    typeof vi.fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listIssues = vi.fn(() => never<ListIssuesResponse>());
    mockTranslate.mockClear();
    pruneIssueSurfaceViewStates([]);
    mockWsId.current = "ws-1";
    // jsdom has no IntersectionObserver; the table footer sentinel constructs
    // one on mount. A stub that never fires keeps the sentinel inert, so the
    // structure loop is the only automatic pagination driver under test.
    vi.stubGlobal(
      "IntersectionObserver",
      class {
        observe() {}
        unobserve() {}
        disconnect() {}
      },
    );
  });

  afterEach(() => {
    cleanup();
    qc.clear();
    pruneIssueSurfaceViewStates([]);
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  it("does not materialize the legacy offset window and starts one cursor root branch", async () => {
    const { getIssueSurfaceViewStore } = await import(
      "@multica/core/issues/stores/surface-view-store"
    );
    const store = getIssueSurfaceViewStore("project:pt");
    store.getState().setViewMode("table");
    if (!store.getState().agentRunningFilter) {
      store.getState().toggleAgentRunningFilter();
    }

    const runningIssues = Array.from({ length: 250 }, (_, index) => ({
      ...makeIssue(`run-${index}`, `Running ${index}`, "pt"),
      status: "in_progress" as const,
    }));
    const listIssueTableRows = vi.fn(() => never());
    setApiInstance({
      listIssues,
      listIssueTableRows,
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => never()),
      getAgentTaskSnapshot: vi.fn(() =>
        Promise.resolve(
          runningIssues.map((issue, index) => ({
            id: `task-${index}`,
            agent_id: `agent-${index}`,
            issue_id: issue.id,
            status: "running",
          })) as unknown as AgentTask[],
        ),
      ),
      getChildIssueProgress: vi.fn(() => never()),
      listProperties: vi.fn(() => never()),
      listMembers: vi.fn(() => never()),
      listAgents: vi.fn(() => never()),
      listSquads: vi.fn(() => never()),
    } as unknown as ApiClient);

    render(
      <QueryClientProvider client={qc}>
        <IssueSurface
          scope={{ type: "project", projectId: "pt" }}
          modes={["table"]}
          renderHeader={() => null}
          renderLoading={() => <div data-testid="surface-loading" />}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalledTimes(1));
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({
        group: { kind: "none" },
        group_key: null,
        parent_id: null,
        query: expect.objectContaining({
          filters: expect.objectContaining({ working_only: true }),
        }),
      }),
    );
    expect(listIssues).not.toHaveBeenCalled();
  });

  it("keeps loaded rows when a continuation page reports zero", async () => {
    const { getIssueSurfaceViewStore } = await import(
      "@multica/core/issues/stores/surface-view-store"
    );
    const store = getIssueSurfaceViewStore("project:pt-pages");
    store.getState().setViewMode("table");
    const first = makeIssue("page-1", "First cursor row", "pt-pages");
    const second = makeIssue("page-2", "Second cursor row", "pt-pages");
    const listIssueTableRows = vi.fn((request: IssueTableRowsRequest) =>
      Promise.resolve(
        request.page?.cursor == null
          ? {
              query_fingerprint: "sha256:pages",
              group_key: null,
              parent_id: null,
              total: 2,
              rows: [{ issue: first, direct_child_count: 0 }],
              branch_total: 1,
              next_cursor: "cursor-2",
            }
          : {
              query_fingerprint: "sha256:pages",
              group_key: null,
              parent_id: null,
              total: 0,
              rows: [{ issue: second, direct_child_count: 0 }],
              branch_total: 1,
              next_cursor: null,
            },
      ),
    );
    setApiInstance({
      listIssues,
      listIssueTableRows,
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    render(
      <QueryClientProvider client={qc}>
        <IssueSurface
          scope={{ type: "project", projectId: "pt-pages" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("First cursor row");
    const loadMoreButton = document.querySelector<HTMLButtonElement>(
      "tbody button.sticky",
    );
    expect(loadMoreButton).not.toBeNull();
    fireEvent.click(loadMoreButton!);

    await screen.findByText("Second cursor row");
    expect(listIssueTableRows).toHaveBeenCalledWith(
      expect.objectContaining({ page: { limit: 50, cursor: "cursor-2" } }),
    );
    expect(screen.getByText("First cursor row")).toBeInTheDocument();
  });

  it("feeds loaded Table rows to the shared batch toolbar", async () => {
    const { getIssueSurfaceViewStore } = await import(
      "@multica/core/issues/stores/surface-view-store"
    );
    const store = getIssueSurfaceViewStore("project:pt-batch");
    store.getState().setViewMode("table");
    const issue = makeIssue("table-selected", "Loaded Table issue", "pt-batch");

    setApiInstance({
      listIssues,
      listIssueTableRows: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:table-batch",
          group_key: null,
          parent_id: null,
          total: 1,
          rows: [{ issue, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: null,
        }),
      ),
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { container } = render(
      <QueryClientProvider client={qc}>
        <IssueSurface
          scope={{ type: "project", projectId: "pt-batch" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="always"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Loaded Table issue");
    const checkboxes = screen.getAllByRole("checkbox");
    fireEvent.click(checkboxes[1]!);

    await waitFor(() => {
      expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
    });
  });

  it("keeps the previous Table rows painted while a new sort is loading", async () => {
    const { getIssueSurfaceViewStore } = await import(
      "@multica/core/issues/stores/surface-view-store"
    );
    const store = getIssueSurfaceViewStore("project:pt-sort-transition");
    store.getState().setViewMode("table");
    const issue = makeIssue(
      "table-sort-placeholder",
      "Table row kept during sort",
      "pt-sort-transition",
    );
    const listIssueTableRows = vi.fn((request: IssueTableRowsRequest) =>
      request.query.sort.field === "position"
        ? Promise.resolve({
            query_fingerprint: "sha256:initial-sort",
            group_key: null,
            parent_id: null,
            total: 1,
            rows: [{ issue, direct_child_count: 0 }],
            branch_total: 1,
            next_cursor: null,
          })
        : never(),
    );
    setApiInstance({
      listIssues,
      listIssueTableRows,
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    render(
      <QueryClientProvider client={qc}>
        <IssueSurface
          scope={{ type: "project", projectId: "pt-sort-transition" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="never"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Table row kept during sort");
    act(() => store.getState().setSortBy("title"));
    await waitFor(() => expect(listIssueTableRows).toHaveBeenCalledTimes(2));
    expect(screen.getByText("Table row kept during sort")).toBeInTheDocument();
  });

  it("keeps selected Table rows in the batch universe after their group collapses", async () => {
    const { getIssueSurfaceViewStore } = await import(
      "@multica/core/issues/stores/surface-view-store"
    );
    const store = getIssueSurfaceViewStore("project:pt-collapsed-batch");
    store.getState().setViewMode("table");
    store.getState().setTableGrouping("status");
    const issue = makeIssue(
      "table-collapsed-selected",
      "Selected issue in collapsed group",
      "pt-collapsed-batch",
    );

    vi.stubGlobal(
      "IntersectionObserver",
      class {
        private readonly callback: IntersectionObserverCallback;
        constructor(callback: IntersectionObserverCallback) {
          this.callback = callback;
        }
        observe(target: Element) {
          this.callback(
            [{ isIntersecting: true, target } as IntersectionObserverEntry],
            this as unknown as IntersectionObserver,
          );
        }
        unobserve() {}
        disconnect() {}
        takeRecords() {
          return [];
        }
        root = null;
        rootMargin = "0px";
        thresholds = [0];
      },
    );

    setApiInstance({
      listIssues,
      listIssueTableGroups: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:collapsed-groups",
          total: 1,
          groups: [
            {
              key: "status:todo",
              value: { kind: "status", status: "todo" },
              count: 1,
            },
          ],
          next_cursor: null,
        }),
      ),
      listIssueTableRows: vi.fn(() =>
        Promise.resolve({
          query_fingerprint: "sha256:collapsed-rows",
          group_key: "status:todo",
          parent_id: null,
          total: 1,
          rows: [{ issue, direct_child_count: 0 }],
          branch_total: 1,
          next_cursor: null,
        }),
      ),
      listIssueTableFacets: vi.fn(() => never()),
      listGroupedIssues: vi.fn(() => never()),
      listProjects: vi.fn(() => Promise.resolve([])),
      getAgentTaskSnapshot: vi.fn(() => Promise.resolve([])),
      getChildIssueProgress: vi.fn(() => Promise.resolve([])),
      listProperties: vi.fn(() => Promise.resolve({ properties: [] })),
      listMembers: vi.fn(() => Promise.resolve([])),
      listAgents: vi.fn(() => Promise.resolve([])),
      listSquads: vi.fn(() => Promise.resolve([])),
    } as unknown as ApiClient);

    const { container } = render(
      <QueryClientProvider client={qc}>
        <IssueSurface
          scope={{ type: "project", projectId: "pt-collapsed-batch" }}
          modes={["table"]}
          renderHeader={() => null}
          batchToolbar="always"
        />
      </QueryClientProvider>,
    );

    await screen.findByText("Selected issue in collapsed group");
    fireEvent.click(screen.getAllByRole("checkbox")[1]!);
    await waitFor(() => {
      expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
    });

    fireEvent.click(screen.getByRole("button", { name: "translated1" }));
    await waitFor(() => {
      expect(screen.queryByText("Selected issue in collapsed group")).toBeNull();
    });
    expect(container.querySelector(".fixed.bottom-6")).not.toBeNull();
  });
});
