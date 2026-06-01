import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { Issue, ListIssuesParams, ListIssuesResponse } from "../types";
import {
  CHILDREN_BY_PARENTS_CHUNK_SIZE,
  PROJECT_GANTT_MAX_ISSUES,
  PROJECT_GANTT_PAGE_LIMIT,
  childrenByParentsOptions,
  issueKeys,
  mergeAssigneeGroups,
  mergeIssueCaches,
  projectGanttIssuesOptions,
  viewIssueListOptions,
} from "./queries";
import type { GroupedIssuesResponse, ListIssuesCache } from "../types";

const WS_ID = "ws-1";
const PROJECT_ID = "project-1";

function makeIssue(idx: number): Issue {
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
    start_date: "2026-05-01T00:00:00Z",
    due_date: null,
    labels: [],
    metadata: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
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

describe("mergeIssueCaches", () => {
  const withStatus = (issue: Issue, status: string): Issue => ({ ...issue, status: status as Issue["status"] });
  const cacheOf = (status: string, issues: Issue[], total: number): ListIssuesCache => ({
    byStatus: { [status as Issue["status"]]: { issues, total } },
  });

  it("passes a single cache through untouched (preserves server totals)", () => {
    const cache = cacheOf("todo", [makeIssue(1)], 42);
    // Same reference back — single-source pagination relies on the server total.
    expect(mergeIssueCaches([cache])).toBe(cache);
    expect(mergeIssueCaches([cache]).byStatus.todo?.total).toBe(42);
  });

  it("unions multiple caches per status, deduping by id and preserving order", () => {
    const a = cacheOf("todo", [withStatus(makeIssue(1), "todo"), withStatus(makeIssue(2), "todo")], 2);
    const b = cacheOf("todo", [withStatus(makeIssue(2), "todo"), withStatus(makeIssue(3), "todo")], 2);
    const merged = mergeIssueCaches([a, b]);
    expect(merged.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
      "issue-3",
    ]);
    // Multi-source: total is the merged length, not a server count.
    expect(merged.byStatus.todo?.total).toBe(3);
  });
});

describe("mergeAssigneeGroups", () => {
  const group = (
    assigneeId: string | null,
    issues: Issue[],
  ): GroupedIssuesResponse["groups"][number] => ({
    id: assigneeId ?? "none",
    assignee_type: assigneeId ? "member" : null,
    assignee_id: assigneeId,
    issues,
    total: issues.length,
  });

  it("passes a single response through untouched", () => {
    const res: GroupedIssuesResponse = { groups: [group("u1", [makeIssue(1)])] };
    expect(mergeAssigneeGroups([res])).toBe(res);
  });

  it("merges groups by (assignee_type, assignee_id), deduping issues", () => {
    const r1: GroupedIssuesResponse = { groups: [group("u1", [makeIssue(1), makeIssue(2)])] };
    const r2: GroupedIssuesResponse = { groups: [group("u1", [makeIssue(2), makeIssue(3)])] };
    const merged = mergeAssigneeGroups([r1, r2]);
    expect(merged.groups).toHaveLength(1);
    expect(merged.groups[0]!.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
      "issue-3",
    ]);
    expect(merged.groups[0]!.total).toBe(3);
  });
});

describe("viewIssueListOptions", () => {
  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("keys distinct cache entries per viewId even with identical filters", () => {
    const a = viewIssueListOptions(WS_ID, "view-a", { statuses: ["todo"] });
    const b = viewIssueListOptions(WS_ID, "view-b", { statuses: ["todo"] });
    expect(a.queryKey).not.toEqual(b.queryKey);
  });

  it("fans out one fetch per any_of branch and dedupes the union by id", async () => {
    // Two branches that both surface the same todo issue; the union must
    // dedupe it. Each branch fetches one page per paginated status, so the
    // call count is (#branches × #statuses).
    const listIssues = vi
      .fn<(params?: ListIssuesParams) => Promise<ListIssuesResponse>>()
      .mockImplementation(async (params) => {
        if (params?.status !== "todo") return { issues: [], total: 0 };
        if (params.assignee_filters) return { issues: [makeIssue(1)], total: 1 };
        if (params.creator_filters) return { issues: [makeIssue(1), makeIssue(2)], total: 2 };
        return { issues: [], total: 0 };
      });
    installFakeApi(listIssues);

    // fetchQuery returns the raw queryFn result (the bucketed cache); `select`
    // only runs for useQuery observers, so read the bucket directly here. The
    // options' `select` narrows the inferred return type to Issue[], so reach
    // into the untransformed cache via the query cache instead.
    const opts = viewIssueListOptions(WS_ID, "v", {
      any_of: [
        { assignee_filters: ["member:{me}"] },
        { creator_filters: ["member:{me}"] },
      ],
    });
    await qc.fetchQuery(opts);
    const cache = qc.getQueryData<ListIssuesCache>(opts.queryKey);

    expect(cache?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
    ]);
  });
});
