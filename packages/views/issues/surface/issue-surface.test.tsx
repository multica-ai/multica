/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { pruneIssueSurfaceViewStates } from "@multica/core/issues/stores/surface-view-store";
import type {
  AgentTask,
  Issue,
  ListIssuesParams,
  ListIssuesResponse,
} from "@multica/core/types";
import { IssueSurface } from "./issue-surface";

// Mutable so tests can simulate a workspace switch — the workspace layout
// does not remount its children on switch, so the surface must handle the
// wsId change itself.
const mockWsId = vi.hoisted(() => ({ current: "ws-1" }));
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
  useT: () => ({ t: () => "translated", i18n: { language: "en" } }),
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

  it("requests each offset exactly once while the agents-working filter is on", async () => {
    // With the filter on, the working window IS the main table window (same
    // query key). The data hook's chip loop and TableView's structure loop
    // used to both answer the same render snapshot with fetchNextPage(),
    // and the default cancel/restart semantics doubled every offset's HTTP
    // request (round-6 review R1). Ownership now belongs to the structure
    // loop alone, and all auto callers use cancelRefetch: false.
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
    const idsOffsets: number[] = [];
    listIssues.mockImplementation((params?: ListIssuesParams) => {
      // A PRESENT-but-empty ids facet is the pre-snapshot state and returns
      // an empty window (server semantics) — only the real running-set key's
      // offsets count toward the uniqueness assertion.
      if (params?.ids && params.ids.length > 0) {
        const offset = params.offset ?? 0;
        idsOffsets.push(offset);
        return Promise.resolve({
          issues: runningIssues.slice(offset, offset + 100),
          total: runningIssues.length,
        });
      }
      return Promise.resolve({ issues: [], total: 0 });
    });
    setApiInstance({
      listIssues,
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

    // The structure loop (hierarchy is on by default, 250 ≤ ceiling)
    // materializes the window without user interaction.
    await waitFor(() => expect(idsOffsets).toContain(200), { timeout: 5000 });
    // Let any straggling (buggy) second responder fire before asserting.
    await new Promise((resolve) => setTimeout(resolve, 100));

    expect([...idsOffsets].sort((a, b) => a - b)).toEqual([0, 100, 200]);
  });
});
