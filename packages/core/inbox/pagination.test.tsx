// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { InboxPage, InboxItem, Issue } from "../types";
import { issueKeys } from "../issues/queries";
import { issueChangedDims } from "../issues/surface/membership";
import { applyIssueChange, rollbackIssueChange, invalidateStaleListKeys } from "../issues/cache-coordinator";
import { EMPTY_INBOX_FILTERS } from "./filter-store";
import { archivedInboxPagesOptions, inboxFacetsOptions, inboxKeys, inboxLookupOptions, inboxPagesOptions } from "./queries";
import { useArchiveInbox, useMarkAllInboxRead, useUnarchiveInbox, useMarkInboxRead } from "./mutations";
import { onInboxNew, onInboxIssueDeleted, onInboxIssueStatusChanged } from "./ws-updaters";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws" }));

function item(id: string, archived = true): InboxItem {
  return { id, workspace_id: "ws", recipient_type: "member", recipient_id: "user", actor_type: "system", actor_id: null,
    type: "mentioned", severity: "info", issue_id: id, title: id, body: null, issue_status: "done", issue_priority: "high",
    read: false, archived, created_at: "2026-09-01T00:00:00Z", details: null };
}
const active = (id: string) => item(id, false);
const page = (items: InboxItem[], nextCursor: string | null = null): InboxPage => ({ items, nextCursor, hasMore: !!nextCursor });
function setup() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false, staleTime: Infinity }, mutations: { retry: false } } });
  const wrapper = ({ children }: { children: ReactNode }) => <QueryClientProvider client={qc}>{children}</QueryClientProvider>;
  return { qc, wrapper };
}

describe("archived inbox pagination", () => {
  it("loads subsequent pages, preserves rows on failure, and retries the same cursor", async () => {
    const { qc, wrapper } = setup();
    const listArchivedInboxPage = vi.fn().mockResolvedValueOnce(page([item("first")], "next"))
      .mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(page([item("second")]));
    setApiInstance({ listArchivedInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS)), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await act(() => result.current.fetchNextPage());
    expect(result.current.data?.pages[0]?.items[0]?.id).toBe("first");
    await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
    await act(() => result.current.fetchNextPage());
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2));
    expect(listArchivedInboxPage.mock.calls.slice(1).map((call) => call[1].cursor)).toEqual(["next", "next"]);
    expect(result.current.hasNextPage).toBe(false);
    unmount(); qc.clear();
  });

  it("marks unopened archives stale without fetching until the archive is opened", async () => {
    const { qc, wrapper } = setup();
    const listArchivedInboxPage = vi.fn(async () => page([item("archived")]));
    setApiInstance({ listArchivedInboxPage } as unknown as ApiClient);
    const { result, rerender, unmount } = renderHook(({ open }) => useInfiniteQuery({
      ...archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS), enabled: open,
    }), { wrapper, initialProps: { open: false } });
    await act(() => onInboxNew(qc, "ws", item("new")));
    expect(listArchivedInboxPage).not.toHaveBeenCalled();
    rerender({ open: true });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    expect(listArchivedInboxPage).toHaveBeenCalledOnce();
    unmount(); qc.clear();
  });

  it("patches and rolls back restore across pages, filters and deep links", async () => {
    const { qc, wrapper } = setup();
    const keys = [archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey,
      archivedInboxPagesOptions("ws", { ...EMPTY_INBOX_FILTERS, priorities: ["high"] }).queryKey];
    const initial: InfiniteData<InboxPage> = { pages: [page([item("first")], "next"), page([item("target")])], pageParams: [null, "next"] };
    for (const key of keys) qc.setQueryData(key, initial);
    const lookupKey = [...inboxKeys.lookup("ws"), "target"];
    qc.setQueryData(lookupKey, page([item("target")]));
    let reject!: (reason: Error) => void;
    setApiInstance({ unarchiveInbox: () => new Promise((_resolve, fail) => { reject = fail; }) } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useUnarchiveInbox(), { wrapper });
    act(() => result.current.mutate("target"));
    await waitFor(() => expect(qc.getQueryData<InboxPage>(lookupKey)?.items[0]?.archived).toBe(false));
    for (const key of keys) expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[1]?.items[0]?.archived).toBe(false);
    act(() => reject(new Error("restore failed")));
    await waitFor(() => expect(result.current.isError).toBe(true));
    for (const key of keys) expect(qc.getQueryData(key)).toEqual(initial);
    expect(qc.getQueryData<InboxPage>(lookupKey)?.items[0]?.archived).toBe(true);
    unmount(); qc.clear();
  });

  it("rolls back issue projections across archive caches without fetching uncommitted state", () => {
    const { qc } = setup();
    const key = archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    const initial = { pages: [page([item("target")])], pageParams: [null] };
    qc.setQueryData(key, initial);
    const result = applyIssueChange(qc, "ws", "target", { priority: "low" }, { changed: issueChangedDims({ priority: "low" }) });
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[0]?.items[0]?.issue_priority).toBe("low");
    expect(qc.getQueryState(key)?.isInvalidated).toBe(false);
    expect(result.staleKeys).toContainEqual(key);
    rollbackIssueChange(qc, "ws", "target", result);
    expect(qc.getQueryData(key)).toEqual(initial);
    qc.clear();
  });

  it("replaces a first page request that predates an issue projection change", async () => {
    const { qc, wrapper } = setup();
    let resolveOld!: (value: InboxPage) => void;
    const listArchivedInboxPage = vi.fn()
      .mockImplementationOnce(() => new Promise<InboxPage>((resolve) => { resolveOld = resolve; }))
      .mockResolvedValue(page([{ ...item("target"), issue_priority: "low" }]));
    setApiInstance({ listArchivedInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS)), { wrapper });
    await waitFor(() => expect(listArchivedInboxPage).toHaveBeenCalledOnce());
    act(() => {
      const change = applyIssueChange(qc, "ws", "target", { priority: "low" }, { changed: issueChangedDims({ priority: "low" }) });
      invalidateStaleListKeys(qc, change.staleKeys);
    });
    await waitFor(() => expect(listArchivedInboxPage).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.data?.pages[0]?.items[0]?.issue_priority).toBe("low"));
    await act(async () => resolveOld(page([item("target")])));
    expect(result.current.data?.pages[0]?.items[0]?.issue_priority).toBe("low");
    unmount(); qc.clear();
  });

  it("updates read state on later pages and invalidates server-owned filter membership on issue events", async () => {
    const { qc, wrapper } = setup();
    const key = archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    qc.setQueryData<InfiniteData<InboxPage>>(key, { pages: [page([item("first")], "next"), page([item("target")])], pageParams: [null, "next"] });
    setApiInstance({ markInboxRead: async () => ({ ...item("target"), read: true }) } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useMarkInboxRead(), { wrapper });
    await act(() => result.current.mutateAsync("target"));
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[1]?.items[0]?.read).toBe(true);
    onInboxIssueStatusChanged(qc, "ws", "target", "todo");
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[1]?.items[0]?.issue_status).toBe("todo");
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    await onInboxIssueDeleted(qc, "ws", "target");
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[1]?.items).toEqual([]);
    unmount(); qc.clear();
  });
});

describe("active inbox pagination", () => {
  const statusFiltered = { ...EMPTY_INBOX_FILTERS, statuses: ["done" as const] };
  const twoPages = (): InfiniteData<InboxPage> => ({
    pages: [page([active("first")], "next"), page([active("target")])], pageParams: [null, "next"],
  });

  it("loads subsequent pages from the active endpoint and retries the same cursor", async () => {
    const { qc, wrapper } = setup();
    const listInboxPage = vi.fn().mockResolvedValueOnce(page([active("first")], "next"))
      .mockRejectedValueOnce(new Error("offline")).mockResolvedValueOnce(page([active("second")]));
    setApiInstance({ listInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(inboxPagesOptions("ws", statusFiltered)), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    await act(() => result.current.fetchNextPage());
    await waitFor(() => expect(result.current.isFetchNextPageError).toBe(true));
    expect(result.current.data?.pages[0]?.items[0]?.id).toBe("first");
    await act(() => result.current.fetchNextPage());
    await waitFor(() => expect(result.current.data?.pages).toHaveLength(2));
    expect(listInboxPage.mock.calls.map((call) => call[0])).toEqual([statusFiltered, statusFiltered, statusFiltered]);
    expect(listInboxPage.mock.calls.slice(1).map((call) => call[1].cursor)).toEqual(["next", "next"]);
    expect(result.current.hasNextPage).toBe(false);
    unmount(); qc.clear();
  });

  it("archives a whole group across pages, filters and deep links, and rolls back", async () => {
    const { qc, wrapper } = setup();
    const keys = [inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey, inboxPagesOptions("ws", statusFiltered).queryKey];
    const sibling = { ...active("sibling"), issue_id: "target" };
    const initial: InfiniteData<InboxPage> = { pages: [page([sibling, active("first")], "next"), page([active("target")])], pageParams: [null, "next"] };
    for (const key of keys) qc.setQueryData(key, initial);
    const lookupKey = inboxLookupOptions("ws", "target").queryKey;
    qc.setQueryData(lookupKey, page([active("target")]));
    let reject!: (reason: Error) => void;
    setApiInstance({ archiveInbox: () => new Promise((_resolve, fail) => { reject = fail; }) } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useArchiveInbox(), { wrapper });
    act(() => result.current.mutate("target"));
    await waitFor(() => expect(qc.getQueryData<InboxPage>(lookupKey)?.items[0]?.archived).toBe(true));
    for (const key of keys) {
      const pages = qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages;
      expect(pages?.[0]?.items.map((row) => row.archived)).toEqual([true, false]);
      expect(pages?.[1]?.items[0]?.archived).toBe(true);
    }
    act(() => reject(new Error("archive failed")));
    await waitFor(() => expect(result.current.isError).toBe(true));
    for (const key of keys) expect(qc.getQueryData(key)).toEqual(initial);
    expect(qc.getQueryData<InboxPage>(lookupKey)?.items[0]?.archived).toBe(false);
    unmount(); qc.clear();
  });

  it("marks later pages read, one row or all of them", async () => {
    const { qc, wrapper } = setup();
    const key = inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    qc.setQueryData(key, twoPages());
    setApiInstance({
      markInboxRead: async () => ({ ...active("target"), read: true }),
      markAllInboxRead: async () => ({ count: 2 }),
    } as unknown as ApiClient);
    const read = renderHook(() => useMarkInboxRead(), { wrapper });
    await act(() => read.result.current.mutateAsync("target"));
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages.map((p) => p.items[0]?.read)).toEqual([false, true]);
    const all = renderHook(() => useMarkAllInboxRead(), { wrapper });
    await act(() => all.result.current.mutateAsync());
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages.map((p) => p.items[0]?.read)).toEqual([true, true]);
    read.unmount(); all.unmount(); qc.clear();
  });

  it("re-reads only pages filtering on the changed issue field, and rolls the projection back", () => {
    const { qc } = setup();
    const unfiltered = inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    const byStatus = inboxPagesOptions("ws", statusFiltered).queryKey;
    const byPriority = inboxPagesOptions("ws", { ...EMPTY_INBOX_FILTERS, priorities: ["high"] }).queryKey;
    const lookup = inboxLookupOptions("ws", "target").queryKey;
    const facets = inboxFacetsOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    for (const key of [unfiltered, byStatus, byPriority]) qc.setQueryData(key, twoPages());
    qc.setQueryData(lookup, page([active("target")]));
    qc.setQueryData(facets, { statuses: { done: 2 }, priorities: { high: 2 }, actors: {}, unreadCount: 2 });
    const result = applyIssueChange(qc, "ws", "target", { status: "todo" }, { changed: issueChangedDims({ status: "todo" }) });
    for (const key of [unfiltered, byStatus, byPriority]) {
      expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages[1]?.items[0]?.issue_status).toBe("todo");
    }
    expect(qc.getQueryData<InboxPage>(lookup)?.items[0]?.issue_status).toBe("todo");
    expect(result.staleKeys).toContainEqual(byStatus);
    expect(result.staleKeys).toContainEqual(facets);
    expect(result.staleKeys).not.toContainEqual(unfiltered);
    expect(result.staleKeys).not.toContainEqual(byPriority);
    expect(result.staleKeys).not.toContainEqual(lookup);
    rollbackIssueChange(qc, "ws", "target", result);
    expect(qc.getQueryData(byStatus)).toEqual(twoPages());
    expect(qc.getQueryData<InboxPage>(lookup)?.items[0]?.issue_status).toBe("done");
    qc.clear();
  });

  // A full issue event repeats the issue's unchanged status and priority, and
  // its flags say no membership field moved.
  const titleEdit = { title: "renamed", status: "done", priority: "high" } as const;
  const noFieldMoved = { assignee: false, project: false, status: false };
  const priorityFiltered = { ...EMPTY_INBOX_FILTERS, priorities: ["high" as const] };

  it.each([["status", statusFiltered], ["priority", priorityFiltered]])(
    "leaves a %s-filtered page alone on a title edit of an issue no issue cache holds",
    async (_field, filters) => {
      const { qc, wrapper } = setup();
      const key = inboxPagesOptions("ws", filters).queryKey;
      const listInboxPage = vi.fn(async () => page([active("first"), active("target")]));
      setApiInstance({ listInboxPage } as unknown as ApiClient);
      const { result, unmount } = renderHook(() => useInfiniteQuery(inboxPagesOptions("ws", filters)), { wrapper });
      await waitFor(() => expect(result.current.isSuccess).toBe(true));
      const cancel = vi.spyOn(qc, "cancelQueries");
      act(() => {
        // One issue with loaded inbox rows, one with none.
        for (const id of ["target", "elsewhere"]) {
          const change = applyIssueChange(qc, "ws", id, titleEdit, { changed: noFieldMoved });
          expect(change.staleKeys).not.toContainEqual(key);
          invalidateStaleListKeys(qc, change.staleKeys);
        }
      });
      expect(cancel).not.toHaveBeenCalled();
      expect(qc.getQueryState(key)?.isInvalidated).toBe(false);
      expect(qc.getQueryState(key)?.fetchStatus).toBe("idle");
      expect(listInboxPage).toHaveBeenCalledOnce();
      unmount(); qc.clear();
    },
  );

  it("re-reads a status-filtered page when the event says the status changed", () => {
    const { qc } = setup();
    const byStatus = inboxPagesOptions("ws", statusFiltered).queryKey;
    const byPriority = inboxPagesOptions("ws", priorityFiltered).queryKey;
    for (const key of [byStatus, byPriority]) qc.setQueryData(key, twoPages());
    const change = applyIssueChange(qc, "ws", "elsewhere", { ...titleEdit, status: "todo" }, {
      changed: { ...noFieldMoved, status: true },
    });
    expect(change.staleKeys).toContainEqual(byStatus);
    expect(change.staleKeys).not.toContainEqual(byPriority);
    qc.clear();
  });

  it("re-reads a priority-filtered page when an inbox row or a cached issue holds another priority", () => {
    const { qc } = setup();
    const byStatus = inboxPagesOptions("ws", statusFiltered).queryKey;
    const byPriority = inboxPagesOptions("ws", priorityFiltered).queryKey;
    for (const key of [byStatus, byPriority]) qc.setQueryData(key, twoPages());
    // An inbox row of the issue.
    const fromRow = applyIssueChange(qc, "ws", "target", { ...titleEdit, priority: "low" }, { changed: noFieldMoved });
    expect(fromRow.staleKeys).toContainEqual(byPriority);
    expect(fromRow.staleKeys).not.toContainEqual(byStatus);
    expect(qc.getQueryData<InfiniteData<InboxPage>>(byPriority)?.pages[1]?.items[0]?.issue_priority).toBe("low");
    // The issue itself, for an issue with no loaded inbox row.
    qc.setQueryData(issueKeys.detail("ws", "elsewhere"), { id: "elsewhere", status: "done", priority: "high" } as Issue);
    const fromIssue = applyIssueChange(qc, "ws", "elsewhere", { ...titleEdit, priority: "urgent" }, { changed: noFieldMoved });
    expect(fromIssue.staleKeys).toContainEqual(byPriority);
    expect(fromIssue.staleKeys).not.toContainEqual(byStatus);
    qc.clear();
  });

  it("re-reads unfiltered pages when an issue change lands during a next-page request", async () => {
    const { qc, wrapper } = setup();
    const key = inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    let releaseNext!: (value: InboxPage) => void;
    const listInboxPage = vi.fn()
      .mockResolvedValueOnce(page([active("target")], "next"))
      .mockImplementationOnce(() => new Promise<InboxPage>((resolve) => { releaseNext = resolve; }))
      .mockImplementation(async (_filters: unknown, options: { cursor?: string | null }) =>
        options.cursor ? page([active("second")]) : page([{ ...active("target"), issue_status: "todo" }], "next"));
    setApiInstance({ listInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(inboxPagesOptions("ws", EMPTY_INBOX_FILTERS)), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    act(() => { void result.current.fetchNextPage({ cancelRefetch: false }); });
    await waitFor(() => expect(listInboxPage).toHaveBeenCalledTimes(2));
    act(() => {
      const change = applyIssueChange(qc, "ws", "target", { status: "todo" }, { changed: issueChangedDims({ status: "todo" }) });
      invalidateStaleListKeys(qc, change.staleKeys);
    });
    // The next page read before the patch lands after it and must not put the
    // old status back: the request is cancelled and the loaded page re-read.
    await act(async () => releaseNext(page([active("second")])));
    await waitFor(() => expect(qc.getQueryState(key)?.fetchStatus).toBe("idle"));
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages.map((p) => p.items[0]?.issue_status)).toEqual(["todo"]);
    expect(listInboxPage).toHaveBeenCalledTimes(3);
    unmount(); qc.clear();
  });

  it("leaves a running refetch of unfiltered pages alone on an issue change", async () => {
    const { qc, wrapper } = setup();
    let releaseRefetch!: (value: InboxPage) => void;
    let refetchSignal: AbortSignal | undefined;
    const listInboxPage = vi.fn()
      .mockResolvedValueOnce(page([active("target")]))
      .mockImplementationOnce((_filters: unknown, options: { signal?: AbortSignal }) => {
        refetchSignal = options.signal;
        return new Promise<InboxPage>((resolve) => { releaseRefetch = resolve; });
      });
    setApiInstance({ listInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(inboxPagesOptions("ws", EMPTY_INBOX_FILTERS)), { wrapper });
    await waitFor(() => expect(result.current.isSuccess).toBe(true));
    act(() => { void result.current.refetch(); });
    await waitFor(() => expect(listInboxPage).toHaveBeenCalledTimes(2));
    act(() => {
      const change = applyIssueChange(qc, "ws", "target", { status: "todo" }, { changed: issueChangedDims({ status: "todo" }) });
      expect(change.staleKeys).not.toContainEqual(inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey);
      invalidateStaleListKeys(qc, change.staleKeys);
    });
    expect(refetchSignal?.aborted).toBe(false);
    await act(async () => releaseRefetch(page([{ ...active("target"), issue_status: "todo" }])));
    await waitFor(() => expect(result.current.isFetching).toBe(false));
    expect(listInboxPage).toHaveBeenCalledTimes(2);
    unmount(); qc.clear();
  });

  it("keeps pages untouched by an issue event identical and still owed a refetch", async () => {
    const { qc } = setup();
    const key = inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    qc.setQueryData(key, twoPages());
    await qc.invalidateQueries({ queryKey: key, refetchType: "none" });
    const before = qc.getQueryData(key);
    onInboxIssueStatusChanged(qc, "ws", "unrelated-issue", "todo");
    expect(qc.getQueryData(key)).toBe(before);
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    qc.clear();
  });

  it("drops a deleted issue from every active page and re-reads the paged caches", async () => {
    const { qc } = setup();
    const key = inboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    qc.setQueryData(key, twoPages());
    await onInboxIssueDeleted(qc, "ws", "target");
    expect(qc.getQueryData<InfiniteData<InboxPage>>(key)?.pages.map((p) => p.items.map((row) => row.id))).toEqual([["first"], []]);
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    qc.clear();
  });

  it("replaces a first filtered page request that predates an issue change", async () => {
    const { qc, wrapper } = setup();
    let resolveOld!: (value: InboxPage) => void;
    const listInboxPage = vi.fn()
      .mockImplementationOnce(() => new Promise<InboxPage>((resolve) => { resolveOld = resolve; }))
      .mockResolvedValue(page([]));
    setApiInstance({ listInboxPage } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useInfiniteQuery(inboxPagesOptions("ws", statusFiltered)), { wrapper });
    await waitFor(() => expect(listInboxPage).toHaveBeenCalledOnce());
    act(() => {
      const change = applyIssueChange(qc, "ws", "target", { status: "todo" }, { changed: issueChangedDims({ status: "todo" }) });
      invalidateStaleListKeys(qc, change.staleKeys);
    });
    await waitFor(() => expect(listInboxPage).toHaveBeenCalledTimes(2));
    await waitFor(() => expect(result.current.data?.pages[0]?.items).toEqual([]));
    await act(async () => resolveOld(page([active("target")])));
    expect(result.current.data?.pages[0]?.items).toEqual([]);
    unmount(); qc.clear();
  });
});
