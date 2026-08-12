import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type {
  Issue,
  IssueTableRowsRequest,
  IssueTableRowsResponse,
  ListIssuesParams,
  ListIssuesResponse,
  SearchIssuesResponse,
} from "../types";
import {
  CHILDREN_BY_PARENTS_CHUNK_SIZE,
  PROJECT_GANTT_MAX_ISSUES,
  PROJECT_GANTT_PAGE_LIMIT,
  childrenByParentsOptions,
  childIssuesOptions,
  issueIdentifierOptions,
  issueKeys,
  issueTableRowPageOptions,
  projectGanttIssuesOptions,
} from "./queries";

const WS_ID = "ws-1";
const PROJECT_ID = "project-1";

function makeIssue(idx: number, overrides: Partial<Issue> = {}): Issue {
  return {
    id: `issue-${idx}`,
    workspace_id: WS_ID,
    number: idx,
    identifier: `MUL-${idx}`,
    title: `Issue ${idx}`,
    description: null,
    status: "todo",
    priority: "none",
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "user-1",
    parent_issue_id: null,
    project_id: PROJECT_ID,
    position: idx,
    stage: null,
    start_date: "2026-05-01T00:00:00Z",
    due_date: null,
    labels: [],
    metadata: {},
  properties: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

// Type-only shim — only the methods the queries.ts code path under test calls.
function installFakeApi(listIssues: (params?: ListIssuesParams) => Promise<ListIssuesResponse>) {
  setApiInstance({ listIssues } as unknown as ApiClient);
}

function installFakeChildrenApi(
  listChildrenByParents: (parentIds: string[]) => Promise<{ issues: Issue[] }>,
) {
  setApiInstance({ listChildrenByParents } as unknown as ApiClient);
}

function installFakeChildApi(
  listChildIssues: (parentId: string) => Promise<{ issues: Issue[] }>,
) {
  setApiInstance({ listChildIssues } as unknown as ApiClient);
}

function installFakeSearchApi(
  searchIssues: (params: { q: string }) => Promise<SearchIssuesResponse>,
) {
  setApiInstance({ searchIssues } as unknown as ApiClient);
}

function makeSearchResult(idx: number, identifier: string) {
  return { ...makeIssue(idx), identifier, match_source: "title" as const };
}

describe("childIssuesOptions", () => {
  it("refetches a cached snapshot when the parent issue is opened again", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { retry: false, staleTime: Infinity } },
    });
    const parentId = "parent-1";
    const oldChild = makeIssue(1, { parent_issue_id: parentId });
    const newChild = makeIssue(2, { parent_issue_id: parentId });
    const listChildIssues = vi.fn().mockResolvedValue({
      issues: [oldChild, newChild],
    });
    installFakeChildApi(listChildIssues);
    qc.setQueryData(issueKeys.children(WS_ID, parentId), [oldChild]);

    const observer = new QueryObserver(
      qc,
      childIssuesOptions(WS_ID, parentId),
    );
    const unsubscribe = observer.subscribe(() => {});

    await vi.waitFor(() => {
      expect(listChildIssues).toHaveBeenCalledWith(parentId);
      expect(observer.getCurrentResult().data).toEqual([oldChild, newChild]);
    });

    unsubscribe();
    qc.clear();
  });
});

describe("issueTableRowPageOptions", () => {
  // Reproduces the "count correct, issue missing until page refresh" bug: a row
  // page gets invalidated while its dynamic useQueries observer is detached, then
  // the observer reattaches. Under the global `staleTime: Infinity` default the
  // page is stale only because it is invalidated, so the reattaching observer
  // MUST refetch it. `refetchOnMount: false` used to suppress that refetch and
  // strand the row stale; `retryOnMount: false` does not.
  const request: IssueTableRowsRequest = {
    query: {
      scope: { kind: "workspace" },
      filters: {},
      sort: { field: "position", direction: "asc" },
    },
    group: { kind: "status" },
    group_key: "todo",
    hierarchy: { enabled: false },
    parent_id: null,
    page: { limit: 50, cursor: null },
  };

  function makeRowsResponse(issues: Issue[]): IssueTableRowsResponse {
    return {
      query_fingerprint: "fp",
      group_key: "todo",
      parent_id: null,
      total: issues.length,
      rows: issues.map((issue) => ({ issue, direct_child_count: 0 })),
      branch_total: issues.length,
      next_cursor: null,
    };
  }

  function rowIssueIds(response: IssueTableRowsResponse | undefined): string[] {
    return response?.rows.map((row) => row.issue.id) ?? [];
  }

  function installFakeTableRowsApi(
    listIssueTableRows: (
      params: IssueTableRowsRequest,
    ) => Promise<IssueTableRowsResponse>,
  ) {
    setApiInstance({ listIssueTableRows } as unknown as ApiClient);
  }

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it("refetches an invalidated page when its observer reattaches", async () => {
    // Global default: server state stays fresh until explicitly invalidated.
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listIssueTableRows = vi
      .fn<
        (params: IssueTableRowsRequest) => Promise<IssueTableRowsResponse>
      >()
      // Head snapshot, then the post-move snapshot that includes the new issue.
      .mockResolvedValueOnce(makeRowsResponse([makeIssue(1)]))
      .mockResolvedValueOnce(makeRowsResponse([makeIssue(1), makeIssue(2)]));
    installFakeTableRowsApi(listIssueTableRows);

    const options = issueTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(listIssueTableRows).toHaveBeenCalledTimes(1);
      expect(rowIssueIds(observer1.getCurrentResult().data)).toEqual([
        "issue-1",
      ]);
    });

    // Observer detaches (sibling branch left the viewport), then the row page is
    // invalidated while no observer is active — it only gets marked stale.
    unsubscribe1();
    await qc.invalidateQueries({ queryKey: options.queryKey });
    const cached = qc.getQueryState(options.queryKey);
    expect(cached?.isInvalidated).toBe(true);
    expect(cached?.fetchStatus).toBe("idle");

    // Observer reattaches: the invalidated page must refetch and pick up issue-2.
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    await vi.waitFor(() => {
      expect(listIssueTableRows).toHaveBeenCalledTimes(2);
      expect(rowIssueIds(observer2.getCurrentResult().data)).toEqual([
        "issue-1",
        "issue-2",
      ]);
    });

    unsubscribe2();
    qc.clear();
  });

  it("keeps an errored page errored on reattach (no auto-retry)", async () => {
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listIssueTableRows = vi
      .fn<
        (params: IssueTableRowsRequest) => Promise<IssueTableRowsResponse>
      >()
      .mockRejectedValue(new Error("boom"));
    installFakeTableRowsApi(listIssueTableRows);

    const options = issueTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("error");
    });
    expect(listIssueTableRows).toHaveBeenCalledTimes(1);
    unsubscribe1();

    // Reattaching an errored page stays idle — `retryOnMount: false` blocks the
    // automatic retry; only an explicit Retry re-runs it. The fetch decision is
    // synchronous, so the observer never enters `fetching`.
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    expect(observer2.getCurrentResult().status).toBe("error");
    expect(observer2.getCurrentResult().fetchStatus).toBe("idle");
    expect(listIssueTableRows).toHaveBeenCalledTimes(1);

    unsubscribe2();
    qc.clear();
  });

  it("does not auto-retry a background-refetch error on a page that still has data", async () => {
    // The tricky case: a page loads OK, then an invalidation-triggered background
    // refetch fails. TanStack flags such a page `isInvalidated: true` (see its
    // "error" reducer), so it is stale AND errored while keeping the old data. A
    // plain `refetchOnMount: true` would re-fire the failing request on every
    // observer reattach; `retryOnMount: false` alone does NOT cover this path
    // because it only guards no-data first-load errors. The `refetchOnMount`
    // status guard is what keeps the errored page stable until an explicit Retry.
    const qc = new QueryClient({
      defaultOptions: { queries: { staleTime: Infinity } },
    });
    const listIssueTableRows = vi
      .fn<
        (params: IssueTableRowsRequest) => Promise<IssueTableRowsResponse>
      >()
      .mockResolvedValueOnce(makeRowsResponse([makeIssue(1)])) // initial load
      .mockRejectedValueOnce(new Error("refetch failed")) // background refetch
      .mockResolvedValueOnce(makeRowsResponse([makeIssue(1), makeIssue(2)])); // explicit Retry
    installFakeTableRowsApi(listIssueTableRows);

    const options = issueTableRowPageOptions(WS_ID, request);

    const observer1 = new QueryObserver(qc, options);
    const unsubscribe1 = observer1.subscribe(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("success");
      expect(rowIssueIds(observer1.getCurrentResult().data)).toEqual([
        "issue-1",
      ]);
    });

    // Invalidate while the observer is active: the background refetch fires and
    // fails, leaving the page errored-with-data and flagged invalidated.
    void qc.invalidateQueries({ queryKey: options.queryKey }).catch(() => {});
    await vi.waitFor(() => {
      expect(observer1.getCurrentResult().status).toBe("error");
    });
    expect(listIssueTableRows).toHaveBeenCalledTimes(2);
    expect(rowIssueIds(observer1.getCurrentResult().data)).toEqual(["issue-1"]);
    expect(qc.getQueryState(options.queryKey)?.isInvalidated).toBe(true);

    // Detach + reattach must NOT re-fire the failing request.
    unsubscribe1();
    const observer2 = new QueryObserver(qc, options);
    const unsubscribe2 = observer2.subscribe(() => {});
    expect(observer2.getCurrentResult().status).toBe("error");
    expect(observer2.getCurrentResult().fetchStatus).toBe("idle");
    expect(listIssueTableRows).toHaveBeenCalledTimes(2);

    // Only an explicit Retry re-runs it — and then the fresh page renders.
    await observer2.refetch();
    expect(listIssueTableRows).toHaveBeenCalledTimes(3);
    expect(rowIssueIds(observer2.getCurrentResult().data)).toEqual([
      "issue-1",
      "issue-2",
    ]);

    unsubscribe2();
    qc.clear();
  });
});

describe("projectGanttIssuesOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("returns the first page directly when it fits under PROJECT_GANTT_PAGE_LIMIT", async () => {
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeIssue(1), makeIssue(2)],
        total: 2,
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(1);
    expect(listIssues).toHaveBeenCalledWith({
      project_id: PROJECT_ID,
      scheduled: true,
      limit: PROJECT_GANTT_PAGE_LIMIT,
      offset: 0,
    });
    expect(data).toHaveLength(2);
  });

  it("loops through pages until total is satisfied (no silent truncation)", async () => {
    const total = PROJECT_GANTT_PAGE_LIMIT + 7;
    const firstPage = Array.from({ length: PROJECT_GANTT_PAGE_LIMIT }, (_, i) =>
      makeIssue(i),
    );
    const secondPage = Array.from({ length: 7 }, (_, i) =>
      makeIssue(PROJECT_GANTT_PAGE_LIMIT + i),
    );

    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (!params) throw new Error("expected params");
        const offset = params.offset ?? 0;
        if (offset === 0)
          return { issues: firstPage, total };
        if (offset === PROJECT_GANTT_PAGE_LIMIT)
          return { issues: secondPage, total };
        throw new Error(`unexpected offset ${offset}`);
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(2);
    expect(data).toHaveLength(total);
  });

  it("stops looping when the server reports a smaller-than-limit page (safety net for total drift)", async () => {
    // Server says `total` is huge but only ever returns short pages — the
    // loop must terminate on the first short page to avoid an infinite fetch.
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeIssue(1)],
        total: PROJECT_GANTT_MAX_ISSUES,
      });
    installFakeApi(listIssues);

    const data = await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));

    expect(listIssues).toHaveBeenCalledTimes(1);
    expect(data).toHaveLength(1);
  });

  it("uses the project-scoped Gantt cache key", () => {
    const options = projectGanttIssuesOptions(WS_ID, PROJECT_ID);
    expect(options.queryKey).toEqual(issueKeys.projectGantt(WS_ID, PROJECT_ID));
  });

  it("threads the assignee-type tab into the request and the cache key", async () => {
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({ issues: [makeIssue(1)], total: 1 });
    installFakeApi(listIssues);

    const agentsTab = projectGanttIssuesOptions(WS_ID, PROJECT_ID, [
      "agent",
      "squad",
    ]);
    await qc.fetchQuery(agentsTab);

    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({
        project_id: PROJECT_ID,
        scheduled: true,
        assignee_types: ["agent", "squad"],
      }),
    );
    // Distinct tabs must not share a cache entry.
    expect(agentsTab.queryKey).not.toEqual(
      projectGanttIssuesOptions(WS_ID, PROJECT_ID).queryKey,
    );
    // The unrestricted tab never sends the param.
    const unrestricted = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({ issues: [], total: 0 });
    installFakeApi(unrestricted);
    await qc.fetchQuery(projectGanttIssuesOptions(WS_ID, PROJECT_ID));
    expect(unrestricted.mock.calls[0]![0]).not.toHaveProperty("assignee_types");
  });
});

describe("childrenByParentsOptions chunking", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("issues a single request when parentIds fit under the chunk size", async () => {
    const parentIds = Array.from({ length: 50 }, (_, i) => `p-${i}`);
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ issues: Issue[] }>>()
      .mockResolvedValue({ issues: [] });
    installFakeChildrenApi(listChildrenByParents);

    await qc.fetchQuery(childrenByParentsOptions(WS_ID, parentIds, qc));

    expect(listChildrenByParents).toHaveBeenCalledTimes(1);
    expect(listChildrenByParents).toHaveBeenCalledWith(parentIds);
  });

  it("chunks parentIds into multiple requests when over the server cap", async () => {
    // 2.5 chunks worth of parents → 3 parallel requests.
    const count = CHILDREN_BY_PARENTS_CHUNK_SIZE * 2 + 17;
    const parentIds = Array.from({ length: count }, (_, i) => `p-${i}`);
    const calls: string[][] = [];
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ issues: Issue[] }>>()
      .mockImplementation(async (ids) => {
        calls.push(ids);
        return { issues: [] };
      });
    installFakeChildrenApi(listChildrenByParents);

    await qc.fetchQuery(childrenByParentsOptions(WS_ID, parentIds, qc));

    expect(listChildrenByParents).toHaveBeenCalledTimes(3);
    expect(calls[0]).toHaveLength(CHILDREN_BY_PARENTS_CHUNK_SIZE);
    expect(calls[1]).toHaveLength(CHILDREN_BY_PARENTS_CHUNK_SIZE);
    expect(calls[2]).toHaveLength(17);
    // Together the chunks must cover every input parent id.
    expect(calls.flat().sort()).toEqual(parentIds.slice().sort());
  });

  it("merges children from all chunks into one grouped map", async () => {
    const parentIds = Array.from(
      { length: CHILDREN_BY_PARENTS_CHUNK_SIZE + 1 },
      (_, i) => `p-${i}`,
    );
    // First chunk returns a child of p-0, second chunk returns a child of
    // the last parent id (which lives alone in chunk 2).
    const lastId = parentIds[parentIds.length - 1]!;
    const listChildrenByParents = vi
      .fn<(ids: string[]) => Promise<{ issues: Issue[] }>>()
      .mockImplementation(async (ids) => {
        if (ids.includes(lastId)) {
          return { issues: [{ ...makeIssue(99), parent_issue_id: lastId }] };
        }
        return { issues: [{ ...makeIssue(1), parent_issue_id: "p-0" }] };
      });
    installFakeChildrenApi(listChildrenByParents);

    const grouped = await qc.fetchQuery(
      childrenByParentsOptions(WS_ID, parentIds, qc),
    );

    expect(grouped.get("p-0")).toHaveLength(1);
    expect(grouped.get(lastId)).toHaveLength(1);
  });
});

describe("issueIdentifierOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("returns the issue whose identifier exactly matches the query", async () => {
    const searchIssues = vi
      .fn<(params: { q: string }) => Promise<SearchIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeSearchResult(7, "MUL-7")],
        total: 1,
      });
    installFakeSearchApi(searchIssues);

    const data = await qc.fetchQuery(issueIdentifierOptions(WS_ID, "MUL-7"));

    expect(data?.id).toBe("issue-7");
    expect(searchIssues).toHaveBeenCalledWith(
      expect.objectContaining({ q: "MUL-7" }),
    );
  });

  it("returns null when no result's identifier matches (wrong prefix / number-only hit)", async () => {
    // Backend number-match returns MUL-7 for a TES-7 query; exact filter rejects it.
    const searchIssues = vi
      .fn<(params: { q: string }) => Promise<SearchIssuesResponse>>()
      .mockResolvedValue({
        issues: [makeSearchResult(7, "MUL-7")],
        total: 1,
      });
    installFakeSearchApi(searchIssues);

    const data = await qc.fetchQuery(issueIdentifierOptions(WS_ID, "TES-7"));

    expect(data).toBeNull();
  });

  it("returns null on an empty (or malformed→empty) search response", async () => {
    const searchIssues = vi
      .fn<(params: { q: string }) => Promise<SearchIssuesResponse>>()
      .mockResolvedValue({ issues: [], total: 0 });
    installFakeSearchApi(searchIssues);

    const data = await qc.fetchQuery(issueIdentifierOptions(WS_ID, "MUL-999"));

    expect(data).toBeNull();
  });

  it("keys the query by workspace and identifier", () => {
    expect(issueKeys.identifier(WS_ID, "MUL-7")).toEqual([
      "issues",
      WS_ID,
      "identifier",
      "MUL-7",
    ]);
  });
});
