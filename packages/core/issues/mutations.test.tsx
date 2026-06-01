/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import { useLoadMoreByAssigneeGroup, useLoadMoreByStatus } from "./mutations";
import {
  issueKeys,
  type IssueSortParam,
} from "./queries";
import type {
  GroupedIssuesResponse,
  Issue,
  ListIssuesCache,
  ListIssuesParams,
  ListGroupedIssuesParams,
  ListIssuesResponse,
} from "../types";

vi.mock("../hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const WS_ID = "ws-1";

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
    project_id: null,
    position: idx,
    start_date: null,
    due_date: null,
    labels: [],
    metadata: {},
    created_at: "2025-01-01T00:00:00Z",
    updated_at: "2025-01-01T00:00:00Z",
    ...overrides,
  };
}

function createWrapper(qc: QueryClient) {
  return function Wrapper({ children }: { children: ReactNode }) {
    return <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  };
}

describe("useLoadMoreByStatus", () => {
  let qc: QueryClient;
  let listIssues: ReturnType<typeof vi.fn<(p?: ListIssuesParams) => Promise<ListIssuesResponse>>>;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listIssues = vi.fn();
    setApiInstance({ listIssues } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("targets the sorted cache key and forwards sort to the API", async () => {
    const sort: IssueSortParam = { sort_by: "priority", sort_direction: "desc" };
    const activeKey = issueKeys.listSorted(WS_ID, sort);
    const seed: ListIssuesCache = {
      byStatus: {
        todo: { issues: [makeIssue(1)], total: 3 },
      },
    };
    qc.setQueryData<ListIssuesCache>(activeKey, seed);

    listIssues.mockResolvedValue({
      issues: [makeIssue(2), makeIssue(3)],
      total: 3,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", undefined, sort),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.hasMore).toBe(true);
    expect(result.current.total).toBe(3);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listIssues).toHaveBeenCalledWith({
      status: "todo",
      limit: 50,
      offset: 1,
      sort_by: "priority",
      sort_direction: "desc",
    });

    const updated = qc.getQueryData<ListIssuesCache>(activeKey);
    expect(updated?.byStatus.todo?.issues).toHaveLength(3);
    expect(updated?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
      "issue-3",
    ]);
  });

  it("ignores a stale cache entry under a different sort", async () => {
    // Stale entry from a previous sort lingers (kept by gcTime / keepPreviousData).
    const staleSort: IssueSortParam = { sort_by: "priority", sort_direction: "desc" };
    qc.setQueryData<ListIssuesCache>(issueKeys.listSorted(WS_ID, staleSort), {
      byStatus: { todo: { issues: [makeIssue(99)], total: 99 } },
    });

    // The active sort cache has its own bucket — load-more must target THIS one.
    const activeSort: IssueSortParam = { sort_by: "position", sort_direction: undefined };
    const activeKey = issueKeys.listSorted(WS_ID, activeSort);
    qc.setQueryData<ListIssuesCache>(activeKey, {
      byStatus: { todo: { issues: [makeIssue(1)], total: 2 } },
    });

    listIssues.mockResolvedValue({
      issues: [makeIssue(2)],
      total: 2,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", undefined, activeSort),
      { wrapper: createWrapper(qc) },
    );

    // total derives from the active key, not the stale one.
    expect(result.current.total).toBe(2);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listIssues).toHaveBeenCalledWith(
      expect.objectContaining({ offset: 1, sort_by: "position" }),
    );

    const active = qc.getQueryData<ListIssuesCache>(activeKey);
    expect(active?.byStatus.todo?.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
    ]);

    // Stale cache is untouched.
    const stale = qc.getQueryData<ListIssuesCache>(issueKeys.listSorted(WS_ID, staleSort));
    expect(stale?.byStatus.todo?.issues.map((i) => i.id)).toEqual(["issue-99"]);
  });

  it("targets the myList scoped cache when myIssues is provided", async () => {
    const sort: IssueSortParam = { sort_by: "title", sort_direction: "asc" };
    const myIssues = { scope: "assigned", filter: { assignee_id: "user-1" } };
    const activeKey = issueKeys.myListSorted(WS_ID, myIssues.scope, myIssues.filter, sort);
    qc.setQueryData<ListIssuesCache>(activeKey, {
      byStatus: { in_progress: { issues: [makeIssue(1, { status: "in_progress" })], total: 2 } },
    });

    listIssues.mockResolvedValue({
      issues: [makeIssue(2, { status: "in_progress" })],
      total: 2,
    });

    const { result } = renderHook(
      () => useLoadMoreByStatus("in_progress", myIssues, sort),
      { wrapper: createWrapper(qc) },
    );

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listIssues).toHaveBeenCalledWith({
      status: "in_progress",
      limit: 50,
      offset: 1,
      sort_by: "title",
      sort_direction: "asc",
      assignee_id: "user-1",
    });

    const updated = qc.getQueryData<ListIssuesCache>(activeKey);
    expect(updated?.byStatus.in_progress?.issues).toHaveLength(2);
  });

  it("works with no sort (matches the {} key used by sort-less callers)", async () => {
    const myIssues = { scope: "actor", filter: { assignee_id: "user-2" } };
    const activeKey = issueKeys.myListSorted(WS_ID, myIssues.scope, myIssues.filter, undefined);
    qc.setQueryData<ListIssuesCache>(activeKey, {
      byStatus: { todo: { issues: [makeIssue(1)], total: 2 } },
    });

    listIssues.mockResolvedValue({ issues: [makeIssue(2)], total: 2 });

    const { result } = renderHook(
      () => useLoadMoreByStatus("todo", myIssues),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.total).toBe(2);
    expect(result.current.hasMore).toBe(true);

    await act(async () => {
      await result.current.loadMore();
    });

    const updated = qc.getQueryData<ListIssuesCache>(activeKey);
    expect(updated?.byStatus.todo?.issues).toHaveLength(2);
  });
});

describe("useLoadMoreByAssigneeGroup", () => {
  let qc: QueryClient;
  let listGroupedIssues: ReturnType<
    typeof vi.fn<(p: ListGroupedIssuesParams) => Promise<GroupedIssuesResponse>>
  >;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    listGroupedIssues = vi.fn();
    setApiInstance({ listGroupedIssues } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("forwards sort to the grouped API and appends into the right group", async () => {
    const sort: IssueSortParam = { sort_by: "priority", sort_direction: "desc" };
    const queryKey = ["custom", "assignee-groups", "ws-1"] as const;
    const seed: GroupedIssuesResponse = {
      groups: [
        {
          id: "assignee:member:user-1",
          assignee_type: "member",
          assignee_id: "user-1",
          issues: [makeIssue(1, { assignee_type: "member", assignee_id: "user-1" })],
          total: 2,
        },
      ],
    };
    qc.setQueryData<GroupedIssuesResponse>(queryKey, seed);

    listGroupedIssues.mockResolvedValue({
      groups: [
        {
          id: "assignee:member:user-1",
          assignee_type: "member",
          assignee_id: "user-1",
          issues: [makeIssue(2, { assignee_type: "member", assignee_id: "user-1" })],
          total: 2,
        },
      ],
    });

    const { result } = renderHook(
      () =>
        useLoadMoreByAssigneeGroup(
          {
            id: "assignee:member:user-1",
            assignee_type: "member",
            assignee_id: "user-1",
          },
          queryKey,
          { statuses: ["todo"] },
          sort,
        ),
      { wrapper: createWrapper(qc) },
    );

    expect(result.current.hasMore).toBe(true);
    expect(result.current.total).toBe(2);

    await act(async () => {
      await result.current.loadMore();
    });

    expect(listGroupedIssues).toHaveBeenCalledWith({
      group_by: "assignee",
      limit: 50,
      offset: 1,
      sort_by: "priority",
      sort_direction: "desc",
      statuses: ["todo"],
      group_assignee_type: "member",
      group_assignee_id: "user-1",
    });

    const updated = qc.getQueryData<GroupedIssuesResponse>(queryKey);
    expect(updated?.groups[0]?.issues.map((i) => i.id)).toEqual([
      "issue-1",
      "issue-2",
    ]);
  });
});
