// @vitest-environment jsdom
import { act, renderHook, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider, useInfiniteQuery, type InfiniteData } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import type { ReactNode } from "react";
import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { ArchivedInboxPage, InboxItem } from "../types";
import { issueChangedDims } from "../issues/surface/membership";
import { applyIssueChange, rollbackIssueChange, invalidateStaleListKeys } from "../issues/cache-coordinator";
import { EMPTY_INBOX_FILTERS } from "./filter-store";
import { archivedInboxPagesOptions, inboxKeys } from "./queries";
import { useUnarchiveInbox, useMarkInboxRead } from "./mutations";
import { onInboxNew, onInboxIssueDeleted, onInboxIssueStatusChanged } from "./ws-updaters";

vi.mock("../hooks", () => ({ useWorkspaceId: () => "ws" }));

function item(id: string): InboxItem {
  return { id, workspace_id: "ws", recipient_type: "member", recipient_id: "user", actor_type: "system", actor_id: null,
    type: "mentioned", severity: "info", issue_id: id, title: id, body: null, issue_status: "done", issue_priority: "high",
    read: false, archived: true, created_at: "2026-09-01T00:00:00Z", details: null };
}
const page = (items: InboxItem[], nextCursor: string | null = null): ArchivedInboxPage => ({ items, nextCursor, hasMore: !!nextCursor });
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
    const initial: InfiniteData<ArchivedInboxPage> = { pages: [page([item("first")], "next"), page([item("target")])], pageParams: [null, "next"] };
    for (const key of keys) qc.setQueryData(key, initial);
    const lookupKey = [...inboxKeys.lookup("ws"), "target"];
    qc.setQueryData(lookupKey, page([item("target")]));
    let reject!: (reason: Error) => void;
    setApiInstance({ unarchiveInbox: () => new Promise((_resolve, fail) => { reject = fail; }) } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useUnarchiveInbox(), { wrapper });
    act(() => result.current.mutate("target"));
    await waitFor(() => expect(qc.getQueryData<ArchivedInboxPage>(lookupKey)?.items[0]?.archived).toBe(false));
    for (const key of keys) expect(qc.getQueryData<InfiniteData<ArchivedInboxPage>>(key)?.pages[1]?.items[0]?.archived).toBe(false);
    act(() => reject(new Error("restore failed")));
    await waitFor(() => expect(result.current.isError).toBe(true));
    for (const key of keys) expect(qc.getQueryData(key)).toEqual(initial);
    expect(qc.getQueryData<ArchivedInboxPage>(lookupKey)?.items[0]?.archived).toBe(true);
    unmount(); qc.clear();
  });

  it("rolls back issue projections across archive caches without fetching uncommitted state", () => {
    const { qc } = setup();
    const key = archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey;
    const initial = { pages: [page([item("target")])], pageParams: [null] };
    qc.setQueryData(key, initial);
    const result = applyIssueChange(qc, "ws", "target", { priority: "low" }, { changed: issueChangedDims({ priority: "low" }) });
    expect(qc.getQueryData<InfiniteData<ArchivedInboxPage>>(key)?.pages[0]?.items[0]?.issue_priority).toBe("low");
    expect(qc.getQueryState(key)?.isInvalidated).toBe(false);
    expect(result.staleKeys).toContainEqual(key);
    rollbackIssueChange(qc, "ws", "target", result);
    expect(qc.getQueryData(key)).toEqual(initial);
    qc.clear();
  });

  it("replaces a first page request that predates an issue projection change", async () => {
    const { qc, wrapper } = setup();
    let resolveOld!: (value: ArchivedInboxPage) => void;
    const listArchivedInboxPage = vi.fn()
      .mockImplementationOnce(() => new Promise<ArchivedInboxPage>((resolve) => { resolveOld = resolve; }))
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
    qc.setQueryData<InfiniteData<ArchivedInboxPage>>(key, { pages: [page([item("first")], "next"), page([item("target")])], pageParams: [null, "next"] });
    setApiInstance({ markInboxRead: async () => ({ ...item("target"), read: true }) } as unknown as ApiClient);
    const { result, unmount } = renderHook(() => useMarkInboxRead(), { wrapper });
    await act(() => result.current.mutateAsync("target"));
    expect(qc.getQueryData<InfiniteData<ArchivedInboxPage>>(key)?.pages[1]?.items[0]?.read).toBe(true);
    onInboxIssueStatusChanged(qc, "ws", "target", "todo");
    expect(qc.getQueryData<InfiniteData<ArchivedInboxPage>>(key)?.pages[1]?.items[0]?.issue_status).toBe("todo");
    expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
    await onInboxIssueDeleted(qc, "ws", "target");
    expect(qc.getQueryData<InfiniteData<ArchivedInboxPage>>(key)?.pages[1]?.items).toEqual([]);
    unmount(); qc.clear();
  });
});
