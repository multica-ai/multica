import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient, QueryObserver } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type {
  Issue,
  ListIssuesParams,
  ListIssuesResponse,
  SearchIssuesResponse,
} from "../types";
import {
  CHILDREN_BY_PARENTS_CHUNK_SIZE,
  ISSUE_FLAT_PAGE_SIZE,
  PROJECT_GANTT_MAX_ISSUES,
  PROJECT_GANTT_PAGE_LIMIT,
  childrenByParentsOptions,
  childIssuesOptions,
  compareIssuesForSort,
  issueFlatExportOptions,
  issueFlatListOptions,
  issueIdentifierOptions,
  issueKeys,
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
});

describe("flat issue table queries", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("loads one offset page for the interactive table window", async () => {
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({ issues: [makeIssue(1)], total: 1 });
    installFakeApi(listIssues);

    const data = await qc.fetchInfiniteQuery(
      issueFlatListOptions(
        WS_ID,
        "project:project-1",
        {
          project_id: PROJECT_ID,
          q: "release train",
          statuses: ["todo", "in_progress"],
          priorities: ["high"],
          assignee_filters: [{ type: "member", id: "member-1" }],
          include_no_assignee: true,
          creator_filters: [{ type: "agent", id: "agent-1" }],
          project_ids: ["project-2"],
          include_no_project: true,
          label_ids: ["label-1"],
          top_level_only: true,
        },
        undefined,
        { sort_by: "updated_at", sort_direction: "desc" },
      ),
    );

    expect(data.pages).toHaveLength(1);
    expect(data.pages[0]?.issues.map((issue) => issue.id)).toEqual([
      "issue-1",
    ]);
    expect(listIssues).toHaveBeenCalledWith({
      project_id: PROJECT_ID,
      q: "release train",
      statuses: ["todo", "in_progress"],
      priorities: ["high"],
      assignee_filters: [{ type: "member", id: "member-1" }],
      include_no_assignee: true,
      creator_filters: [{ type: "agent", id: "agent-1" }],
      project_ids: ["project-2"],
      include_no_project: true,
      label_ids: ["label-1"],
      top_level_only: true,
      sort_by: "updated_at",
      sort_direction: "desc",
      limit: ISSUE_FLAT_PAGE_SIZE,
      offset: 0,
    });
  });

  it("walks every page only for an explicit full CSV export", async () => {
    const first = Array.from({ length: ISSUE_FLAT_PAGE_SIZE }, (_, index) =>
      makeIssue(index + 1),
    );
    const second = [makeIssue(101), makeIssue(102), makeIssue(103)];
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => ({
        issues: (params?.offset ?? 0) === 0 ? first : second,
        total: 103,
      }));
    installFakeApi(listIssues);

    const issues = await qc.fetchQuery(
      issueFlatExportOptions(
        WS_ID,
        "project:project-1",
        { project_id: PROJECT_ID },
        undefined,
        { sort_by: "status", sort_direction: "asc" },
      ),
    );

    expect(issues).toHaveLength(103);
    expect(listIssues).toHaveBeenCalledTimes(2);
    expect(listIssues.mock.calls.map(([params]) => params?.offset)).toEqual([
      0,
      ISSUE_FLAT_PAGE_SIZE,
    ]);
  });

  it("does not silently truncate exports above ten thousand issues", async () => {
    const total = 10_001;
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        const offset = params?.offset ?? 0;
        const count = Math.min(ISSUE_FLAT_PAGE_SIZE, total - offset);
        return {
          issues: Array.from({ length: count }, (_, index) =>
            makeIssue(offset + index + 1),
          ),
          total,
        };
      });
    installFakeApi(listIssues);

    const issues = await qc.fetchQuery(
      issueFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
    );

    expect(issues).toHaveLength(total);
    expect(listIssues).toHaveBeenCalledTimes(101);
    expect(listIssues.mock.calls.at(-1)?.[0]?.offset).toBe(10_000);
  });

  it("keeps paging when the server returns fewer rows than requested", async () => {
    const total = 101;
    const serverPageSize = 40;
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        const offset = params?.offset ?? 0;
        const count = Math.min(serverPageSize, total - offset);
        return {
          issues: Array.from({ length: count }, (_, index) =>
            makeIssue(offset + index + 1),
          ),
          total,
        };
      });
    installFakeApi(listIssues);

    const issues = await qc.fetchQuery(
      issueFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
    );

    expect(issues).toHaveLength(total);
    expect(listIssues.mock.calls.map(([params]) => params?.offset)).toEqual([
      0, 40, 80,
    ]);
  });

  it("fails explicitly if the export endpoint stops advancing offsets", async () => {
    const page = Array.from({ length: ISSUE_FLAT_PAGE_SIZE }, (_, index) =>
      makeIssue(index + 1),
    );
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockResolvedValue({ issues: page, total: ISSUE_FLAT_PAGE_SIZE * 2 });
    installFakeApi(listIssues);

    await expect(
      qc.fetchQuery(
        issueFlatExportOptions(WS_ID, "workspace:all", {}, undefined),
      ),
    ).rejects.toThrow("Issue export pagination did not advance");
    expect(listIssues).toHaveBeenCalledTimes(2);
  });

  it("deduplicates the three My Issues relations and restores global sort order", async () => {
    const shared = makeIssue(1, { status: "done" });
    const backlog = makeIssue(2, { status: "backlog" });
    const todo = makeIssue(3, { status: "todo" });
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (params?.assignee_id) {
          return { issues: [shared], total: 1 };
        }
        if (params?.creator_id) {
          return { issues: [backlog, shared], total: 2 };
        }
        return { issues: [todo], total: 1 };
      });
    installFakeApi(listIssues);

    const issues = await qc.fetchQuery(
      issueFlatExportOptions(
        WS_ID,
        "all",
        {},
        "user-1",
        { sort_by: "status", sort_direction: "asc" },
      ),
    );

    expect(issues.map((issue) => issue.id)).toEqual([
      backlog.id,
      todo.id,
      shared.id,
    ]);
    expect(listIssues).toHaveBeenCalledTimes(3);
  });
});

describe("compareIssuesForSort tie-break", () => {
  it("orders equal sort values by created_at DESC then id DESC (server ORDER BY parity)", () => {
    // Same status AND same created_at — only the id disambiguates. Without a
    // unique final key the relative order would depend on input order, which
    // is exactly the instability that duplicates/drops rows at page
    // boundaries server-side.
    const first = makeIssue(1, { created_at: "2025-01-01T00:00:00Z" });
    const second = makeIssue(2, { created_at: "2025-01-01T00:00:00Z" });
    const sort = { sort_by: "status", sort_direction: "asc" } as const;

    expect(
      [first, second].sort((a, b) => compareIssuesForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["issue-2", "issue-1"]);
    expect(
      [second, first].sort((a, b) => compareIssuesForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["issue-2", "issue-1"]);
  });

  it("applies the id tie-break to created_at sorts as well", () => {
    const first = makeIssue(1, { created_at: "2025-01-01T00:00:00Z" });
    const second = makeIssue(2, { created_at: "2025-01-01T00:00:00Z" });
    const sort = { sort_by: "created_at", sort_direction: "desc" } as const;

    expect(
      [first, second].sort((a, b) => compareIssuesForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["issue-2", "issue-1"]);
    expect(
      [second, first].sort((a, b) => compareIssuesForSort(a, b, sort)).map((i) => i.id),
    ).toEqual(["issue-2", "issue-1"]);
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
