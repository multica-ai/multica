/**
 * @vitest-environment jsdom
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ReactNode } from "react";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import {
  useBatchUpdateIssues,
  useLoadMoreByAssigneeGroup,
  useLoadMoreByStatus,
  useResolveComment,
  useUpdateIssue,
} from "./mutations";
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
  TimelineEntry,
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
    stage: null,
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

describe("useUpdateIssue — optimistic move keeps every bucketed board in sync", () => {
  const sort: IssueSortParam = { sort_by: "position", sort_direction: undefined };
  const myScope = "assigned";
  const myFilter = { assignee_id: "user-1" };
  const wsKey = issueKeys.listSorted(WS_ID, sort);
  // My-Issues AND the Project board both ride this myList cache; a move that
  // only patched the workspace cache snaps back on those boards.
  const myKey = issueKeys.myListSorted(WS_ID, myScope, myFilter, sort);

  let qc: QueryClient;
  let updateIssue: ReturnType<typeof vi.fn<(id: string, data: unknown) => Promise<Issue>>>;

  function makeBucketed(): ListIssuesCache {
    return {
      byStatus: {
        todo: { issues: [makeIssue(1)], total: 1 },
        in_progress: { issues: [], total: 0 },
      },
    };
  }

  function bucketIds(
    key: readonly unknown[],
    status: "todo" | "in_progress",
  ): string[] {
    const c = qc.getQueryData<ListIssuesCache>(key);
    return (c?.byStatus[status]?.issues ?? []).map((i) => i.id);
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    updateIssue = vi.fn();
    setApiInstance({ updateIssue } as unknown as ApiClient);
    qc.setQueryData<ListIssuesCache>(wsKey, makeBucketed());
    qc.setQueryData<ListIssuesCache>(myKey, makeBucketed());
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically moves the card in both the workspace and myList caches", async () => {
    let resolve!: (issue: Issue) => void;
    updateIssue.mockReturnValue(
      new Promise<Issue>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useUpdateIssue(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ id: "issue-1", status: "in_progress", position: 5 });
    });

    // Optimistic state — the regression: myList must move too, not just ws.
    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "todo")).toEqual([]);
      expect(bucketIds(key, "in_progress")).toEqual(["issue-1"]);
    }

    await act(async () => {
      resolve(makeIssue(1, { status: "in_progress", position: 5 }));
    });

    // Authoritative settle keeps the card in place in both caches.
    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "in_progress")).toEqual(["issue-1"]);
    }
  });

  it("rolls both caches back when the request fails", async () => {
    updateIssue.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useUpdateIssue(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ id: "issue-1", status: "in_progress", position: 5 })
        .catch(() => {});
    });

    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "todo")).toEqual(["issue-1"]);
      expect(bucketIds(key, "in_progress")).toEqual([]);
    }
  });

  it("does not invalidate the board list on settle (no refetch flicker)", async () => {
    updateIssue.mockResolvedValue(makeIssue(1, { status: "in_progress", position: 5 }));
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useUpdateIssue(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ id: "issue-1", status: "in_progress", position: 5 });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    // The board list + myList are reconciled surgically, never refetched.
    expect(invalidatedKeys).not.toContainEqual(issueKeys.list(WS_ID));
    expect(invalidatedKeys).not.toContainEqual(issueKeys.myAll(WS_ID));
  });

  it("invalidates myAll on settle when project_id changes (drops the issue from the old project's list)", async () => {
    // A project move makes the issue leave the old project's filtered list. The
    // surgical patch is filter-blind (it never removes a card that no longer
    // matches the list filter), so onSettled must refetch myAll to drop it —
    // unlike a status-only move, which deliberately does not (MUL-3669 / #4548).
    updateIssue.mockResolvedValue(makeIssue(1, { project_id: "project-9" }));
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useUpdateIssue(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ id: "issue-1", project_id: "project-9" });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).toContainEqual(issueKeys.myAll(WS_ID));
  });
});

describe("useBatchUpdateIssues — optimistic patch covers filtered boards too", () => {
  const sort: IssueSortParam = { sort_by: "position", sort_direction: undefined };
  const myScope = "assigned";
  const myFilter = { assignee_id: "user-1" };
  const wsKey = issueKeys.listSorted(WS_ID, sort);
  const myKey = issueKeys.myListSorted(WS_ID, myScope, myFilter, sort);

  let qc: QueryClient;
  let batchUpdateIssues: ReturnType<
    typeof vi.fn<(ids: string[], updates: unknown) => Promise<{ updated: number }>>
  >;

  function makeBucketed(): ListIssuesCache {
    return {
      byStatus: {
        todo: { issues: [makeIssue(1)], total: 1 },
        in_progress: { issues: [], total: 0 },
      },
    };
  }

  function bucketIds(key: readonly unknown[], status: "todo" | "in_progress"): string[] {
    const c = qc.getQueryData<ListIssuesCache>(key);
    return (c?.byStatus[status]?.issues ?? []).map((i) => i.id);
  }

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    batchUpdateIssues = vi.fn();
    setApiInstance({ batchUpdateIssues } as unknown as ApiClient);
    qc.setQueryData<ListIssuesCache>(wsKey, makeBucketed());
    qc.setQueryData<ListIssuesCache>(myKey, makeBucketed());
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("optimistically patches BOTH the workspace and myList caches (not just ws)", async () => {
    let resolve!: (r: { updated: number }) => void;
    batchUpdateIssues.mockReturnValue(
      new Promise<{ updated: number }>((r) => {
        resolve = r;
      }),
    );

    const { result } = renderHook(() => useBatchUpdateIssues(), {
      wrapper: createWrapper(qc),
    });

    act(() => {
      result.current.mutate({ ids: ["issue-1"], updates: { status: "in_progress" } });
    });

    // The regression Howard flagged: batch must move the card on the myList
    // board too, not only the workspace board. onMutate awaits cancelQueries,
    // so the optimistic patch lands a microtask later — wait for it.
    await waitFor(() => {
      for (const key of [wsKey, myKey]) {
        expect(bucketIds(key, "todo")).toEqual([]);
        expect(bucketIds(key, "in_progress")).toEqual(["issue-1"]);
      }
    });

    await act(async () => {
      resolve({ updated: 1 });
    });

    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "in_progress")).toEqual(["issue-1"]);
    }
  });

  it("rolls both caches back when the request fails", async () => {
    batchUpdateIssues.mockRejectedValue(new Error("boom"));

    const { result } = renderHook(() => useBatchUpdateIssues(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current
        .mutateAsync({ ids: ["issue-1"], updates: { status: "in_progress" } })
        .catch(() => {});
    });

    for (const key of [wsKey, myKey]) {
      expect(bucketIds(key, "todo")).toEqual(["issue-1"]);
      expect(bucketIds(key, "in_progress")).toEqual([]);
    }
  });

  it("does not invalidate the board list on settle (no refetch flicker)", async () => {
    batchUpdateIssues.mockResolvedValue({ updated: 1 });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useBatchUpdateIssues(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ ids: ["issue-1"], updates: { status: "in_progress" } });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).not.toContainEqual(issueKeys.list(WS_ID));
  });

  it("invalidates myAll on settle when project_id changes (drops moved issues from the old project's list)", async () => {
    // Mirrors useUpdateIssue: a batch that moves issues between projects must
    // refetch myAll so they leave the old project's filtered list, even though a
    // status-only batch deliberately does not (MUL-3669 / #4548).
    batchUpdateIssues.mockResolvedValue({ updated: 1 });
    const invalidateSpy = vi.spyOn(qc, "invalidateQueries");

    const { result } = renderHook(() => useBatchUpdateIssues(), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({
        ids: ["issue-1"],
        updates: { project_id: "project-9" },
      });
    });

    const invalidatedKeys = invalidateSpy.mock.calls.map((c) => c[0]?.queryKey);
    expect(invalidatedKeys).toContainEqual(issueKeys.myAll(WS_ID));
  });
});

describe("useResolveComment", () => {
  const ISSUE_ID = "issue-1";

  function makeComment(
    id: string,
    parentId: string | null,
    resolvedAt: string | null,
  ): TimelineEntry {
    return {
      type: "comment",
      id,
      actor_type: "member",
      actor_id: "user-1",
      content: id,
      parent_id: parentId,
      comment_type: "comment",
      reactions: [],
      attachments: [],
      created_at: "2026-01-01T00:00:00Z",
      updated_at: "2026-01-01T00:00:00Z",
      resolved_at: resolvedAt,
      resolved_by_type: resolvedAt ? "member" : null,
      resolved_by_id: resolvedAt ? "user-1" : null,
    };
  }

  // Two independent threads on one issue:
  //   root1 ─ a1 (resolved), b1
  //   root2 ─ a2 (resolved)
  function seedTimeline(qc: QueryClient) {
    const entries: TimelineEntry[] = [
      makeComment("root1", null, null),
      makeComment("a1", "root1", "2026-01-01T00:01:00Z"),
      makeComment("b1", "root1", null),
      makeComment("root2", null, null),
      makeComment("a2", "root2", "2026-01-01T00:05:00Z"),
    ];
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID), entries);
  }

  function resolvedIds(qc: QueryClient): string[] {
    const cache = qc.getQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID)) ?? [];
    return cache.filter((e) => e.resolved_at).map((e) => e.id).sort();
  }

  let qc: QueryClient;

  beforeEach(() => {
    qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    setApiInstance({
      resolveComment: vi.fn().mockResolvedValue({ id: "b1" }),
      unresolveComment: vi.fn().mockResolvedValue({ id: "b1" }),
    } as unknown as ApiClient);
  });

  afterEach(() => {
    qc.clear();
    vi.restoreAllMocks();
  });

  it("clears the prior resolution in the same thread when resolving another comment", async () => {
    seedTimeline(qc);

    const { result } = renderHook(() => useResolveComment(ISSUE_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ commentId: "b1", resolved: true });
    });

    // b1 replaces a1 inside thread 1; a2 (thread 2) is untouched.
    expect(resolvedIds(qc)).toEqual(["a2", "b1"]);
  });

  it("does not clear resolutions in other threads", async () => {
    seedTimeline(qc);

    const { result } = renderHook(() => useResolveComment(ISSUE_ID), {
      wrapper: createWrapper(qc),
    });

    // Resolving root1 (thread 1) must leave a2 (thread 2) resolved.
    await act(async () => {
      await result.current.mutateAsync({ commentId: "root1", resolved: true });
    });

    expect(resolvedIds(qc)).toEqual(["a2", "root1"]);
  });

  it("unresolve only clears its own row, never siblings", async () => {
    // Legacy state: two resolved comments coexist in one thread.
    qc.setQueryData<TimelineEntry[]>(issueKeys.timeline(ISSUE_ID), [
      makeComment("root1", null, null),
      makeComment("a1", "root1", "2026-01-01T00:01:00Z"),
      makeComment("b1", "root1", "2026-01-01T00:02:00Z"),
    ]);

    const { result } = renderHook(() => useResolveComment(ISSUE_ID), {
      wrapper: createWrapper(qc),
    });

    await act(async () => {
      await result.current.mutateAsync({ commentId: "b1", resolved: false });
    });

    // Only b1 is cleared; a1 stays resolved (unresolve never mirrors the clear).
    expect(resolvedIds(qc)).toEqual(["a1"]);
  });
});
