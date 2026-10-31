/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { cleanup, screen, waitFor, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { setApiInstance } from "@multica/core/api";
import type { ApiClient } from "@multica/core/api/client";
import { pruneIssueSurfaceViewStates } from "@multica/core/issues/stores/surface-view-store";
import type {
  GitHubPullRequest,
  Issue,
  IssueTableGroupsRequest,
  IssueTableRowsRequest,
} from "@multica/core/types";
import { NavigationProvider, type NavigationAdapter } from "../navigation";
import { renderWithI18n } from "../test/i18n";
import { AwaitingMergePage } from "./awaiting-merge-page";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/paths", async (importOriginal) => {
  const actual = await importOriginal<typeof import("@multica/core/paths")>();
  return {
    ...actual,
    useCurrentWorkspace: () => ({ id: "ws-1", name: "Acme", slug: "acme" }),
    useWorkspacePaths: () => actual.paths.workspace("acme"),
  };
});
vi.mock("@multica/core/auth", () => {
  const state = { user: { id: "user-1", email: "me@acme.test", name: "Me" }, isAuthenticated: true };
  return {
    useAuthStore: Object.assign(
      (selector?: (s: typeof state) => unknown) => (selector ? selector(state) : state),
      { getState: () => state },
    ),
    registerAuthStore: vi.fn(),
    createAuthStore: vi.fn(),
  };
});
// The header has its own tests; this suite is about what the page lists.
vi.mock("./view-header", () => ({ IssueViewHeader: () => null }));
// jsdom has no layout, so the real Virtuoso renders nothing.
vi.mock("react-virtuoso", () => ({
  Virtuoso: ({ data, itemContent, components }: any) => (
    <div>
      {(data ?? []).map((item: any, i: number) => (
        <div key={i}>{itemContent(i, item)}</div>
      ))}
      {components?.Footer ? <components.Footer /> : null}
    </div>
  ),
}));

function makeIssue(id: string, title: string): Issue {
  return {
    id,
    workspace_id: "ws-1",
    number: 1,
    identifier: `MUL-${id}`,
    title,
    description: null,
    status: "in_review",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: null,
    position: 1,
    stage: null,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    properties: {},
    created_at: "2026-10-01T00:00:00Z",
    updated_at: "2026-10-01T00:00:00Z",
  };
}

function makePr(id: string, title: string, over: Partial<GitHubPullRequest> = {}): GitHubPullRequest {
  return {
    id,
    provider: "github",
    workspace_id: "ws-1",
    repo_owner: "acme",
    repo_name: "app",
    number: Number(id.replace(/\D/g, "")) || 1,
    title,
    state: "open",
    html_url: `https://github.com/acme/app/pull/${id}`,
    branch: "feature",
    author_login: "octocat",
    author_avatar_url: null,
    merged_at: null,
    closed_at: null,
    pr_created_at: "2026-10-01T00:00:00Z",
    pr_updated_at: "2026-10-30T00:00:00Z",
    snapshot_available: true,
    mergeable: "mergeable",
    merge_state_status: "clean",
    checks_rollup: "success",
    checks_total: 1,
    checks_passed: 1,
    ...over,
  };
}

const ROWS: Record<string, Issue[]> = {
  "pull_request_state:ready": [makeIssue("1", "Ship the queue")],
  "pull_request_state:needs_attention": [makeIssue("2", "Fix the flaky test"), makeIssue("3", "Rebase the importer")],
  "pull_request_state:checking": [makeIssue("4", "Wait for CI")],
};
const PRS: Record<string, GitHubPullRequest[]> = {
  "1": [makePr("101", "queue: ready PR")],
  // One failing PR is enough to need attention, whatever the other says.
  "2": [makePr("102", "flaky: passing PR"), makePr("103", "flaky: failing PR", { checks_rollup: "failure", checks_failed: 1, checks_passed: 0 })],
  "3": [makePr("104", "importer: behind PR", { merge_state_status: "behind" })],
  "4": [makePr("105", "ci: running PR", { checks_rollup: "pending", checks_running: 1, checks_passed: 0 })],
};

let qc: QueryClient;
let listIssueTableGroups: ReturnType<typeof vi.fn>;
let listIssueTableRows: ReturnType<typeof vi.fn>;

function installApi(groups: { key: string; value: unknown; count: number }[]) {
  listIssueTableGroups = vi.fn(async (_params: IssueTableGroupsRequest) => ({
    query_fingerprint: "sha256:awaiting-merge",
    total: groups.reduce((sum, group) => sum + group.count, 0),
    groups,
    next_cursor: null,
  }));
  listIssueTableRows = vi.fn(async (params: IssueTableRowsRequest) => {
    const rows = ROWS[params.group_key ?? ""] ?? [];
    return {
      query_fingerprint: "sha256:awaiting-merge",
      group_key: params.group_key,
      parent_id: null,
      total: rows.length,
      rows: rows.map((issue) => ({ issue, direct_child_count: 0 })),
      branch_total: rows.length,
      next_cursor: null,
    };
  });
  setApiInstance({
    listIssueStatuses: async () => ({ statuses: [], categories: [], total: 0 }),
    listIssueTableGroups,
    listIssueTableRows,
    listIssueTableFacets: vi.fn(() => new Promise(() => {})),
    listIssuePullRequests: vi.fn(async (issueId: string) => ({ pull_requests: PRS[issueId] ?? [], auto_complete: null })),
    listIssueWakeupSummaries: vi.fn(async () => []),
    listProjects: vi.fn(async () => []),
    getAgentTaskSnapshot: vi.fn(async () => []),
    getChildIssueProgress: vi.fn(async () => []),
    listProperties: vi.fn(async () => ({ properties: [] })),
    listMembers: vi.fn(async () => []),
    listAgents: vi.fn(async () => []),
    listSquads: vi.fn(async () => []),
  } as unknown as ApiClient);
}

function renderPage() {
  const adapter: NavigationAdapter = {
    pathname: "/acme/views/awaiting-merge",
    searchParams: new URLSearchParams(),
    hash: "",
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    getShareableUrl: (path) => path,
  };
  return renderWithI18n(
    <QueryClientProvider client={qc}>
      <NavigationProvider value={adapter}>
        <AwaitingMergePage />
      </NavigationProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  pruneIssueSurfaceViewStates([]);
  // Group heads load once their end scrolls into view.
  vi.stubGlobal(
    "IntersectionObserver",
    class {
      constructor(private readonly callback: IntersectionObserverCallback) {}
      observe(target: Element) {
        this.callback([{ isIntersecting: true, target } as IntersectionObserverEntry], this as unknown as IntersectionObserver);
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
});

afterEach(() => {
  cleanup();
  qc.clear();
  pruneIssueSurfaceViewStates([]);
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("AwaitingMergePage", () => {
  it("asks the server for its fixed scope, PR grouping and newest PR first", async () => {
    installApi([]);
    renderPage();

    await waitFor(() => expect(listIssueTableGroups).toHaveBeenCalled());
    const request = listIssueTableGroups.mock.calls[0]![0] as IssueTableGroupsRequest;
    expect(request.query.scope).toEqual({ kind: "awaiting_merge" });
    expect(request.group).toEqual({ kind: "pull_request_state" });
    expect(request.query.sort).toEqual(expect.objectContaining({ field: "pull_request_updated_at", direction: "desc" }));
  });

  it("lists issues under ready, needs attention and checking, each with the PR that put it there", async () => {
    installApi([
      { key: "pull_request_state:ready", value: { kind: "pull_request_state", value: "ready" }, count: 1 },
      { key: "pull_request_state:needs_attention", value: { kind: "pull_request_state", value: "needs_attention" }, count: 2 },
      { key: "pull_request_state:checking", value: { kind: "pull_request_state", value: "checking" }, count: 1 },
    ]);
    renderPage();

    const ready = await screen.findByTestId("pull-request-queue-group-ready");
    const attention = screen.getByTestId("pull-request-queue-group-needs_attention");
    const checking = screen.getByTestId("pull-request-queue-group-checking");
    expect(ready.compareDocumentPosition(attention) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(attention.compareDocumentPosition(checking) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();

    expect(within(ready).getByText("Ready to merge")).toBeInTheDocument();
    expect(within(attention).getByText("Needs attention")).toBeInTheDocument();
    expect(within(checking).getByText("Checking")).toBeInTheDocument();

    await within(ready).findByText("Ship the queue");
    await within(attention).findByText("Fix the flaky test");
    await within(attention).findByText("Rebase the importer");
    await within(checking).findByText("Wait for CI");

    // The failing PR decides, and the other open one is counted.
    expect(await within(attention).findByTitle("flaky: failing PR")).toBeInTheDocument();
    expect(within(attention).queryByTitle("flaky: passing PR")).toBeNull();
    expect(within(attention).getByText("+1")).toBeInTheDocument();
    expect(await within(ready).findByTitle("queue: ready PR")).toBeInTheDocument();

    // Read-only queue: nothing to select.
    expect(screen.queryAllByRole("checkbox")).toHaveLength(0);
  });

  it("says so when nothing is waiting to merge", async () => {
    installApi([]);
    renderPage();

    expect(await screen.findByText("Nothing waiting to merge")).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /new issue/i })).toBeNull();
  });
});
