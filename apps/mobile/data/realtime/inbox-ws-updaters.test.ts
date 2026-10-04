// @vitest-environment node
import {
  InfiniteQueryObserver,
  QueryClient,
  type InfiniteData,
} from "@tanstack/react-query";
import type { InboxItem, InboxPage } from "@multica/core/types";
import { describe, expect, it, vi } from "vitest";

import { inboxKeys, inboxPagesOptions } from "@/data/queries/inbox";
import {
  dropInboxItemsByIssue,
  patchInboxIssueStatus,
  refreshInboxList,
  refreshInboxUnreadSummary,
} from "./inbox-ws-updaters";

// inbox-ws-updaters imports inboxKeys from data/queries/inbox, which
// transitively imports the native fetch client. Mock it so the Node test never
// loads RN modules — inboxKeys itself is a pure key factory (same reason
// chat-ws-updaters.test.ts does this).
vi.mock("@/data/api", () => ({ api: {} }));

const wsId = "workspace-1";

function item(id: string, issueId: string | null): InboxItem {
  return {
    id,
    workspace_id: wsId,
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: issueId,
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-09-01T08:00:00Z",
    details: null,
  };
}

// Two loaded pages plus a lookup for a group the pages also hold: the cache
// layout after scrolling once and opening a notice.
function seed(qc: QueryClient) {
  qc.setQueryData<InfiniteData<InboxPage, string | null>>(inboxKeys.listPages(wsId), {
    pages: [
      { items: [item("n1", "issue-a")], nextCursor: "cursor-1", hasMore: true },
      { items: [item("n2", "issue-b"), item("n3", "issue-a")], nextCursor: null, hasMore: false },
    ],
    pageParams: [null, "cursor-1"],
  });
  qc.setQueryData<InboxPage>(inboxKeys.listLookup(wsId, "issue-a"), {
    items: [item("n1", "issue-a")],
    nextCursor: null,
    hasMore: false,
  });
}

function pageIds(qc: QueryClient) {
  return qc
    .getQueryData<InfiniteData<InboxPage>>(inboxKeys.listPages(wsId))
    ?.pages.map((page) => page.items.map((i) => i.id));
}

function lookupItems(qc: QueryClient) {
  return qc.getQueryData<InboxPage>(inboxKeys.listLookup(wsId, "issue-a"))?.items;
}

describe("dropInboxItemsByIssue", () => {
  it("drops every row pointing at the deleted issue, on every page and lookup", async () => {
    const qc = new QueryClient();
    seed(qc);

    await dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(pageIds(qc)).toEqual([[], ["n2"]]);
    expect(lookupItems(qc)).toEqual([]);
    // Paging state is the server's; dropping rows must not touch it.
    expect(
      qc.getQueryData<InfiniteData<InboxPage>>(inboxKeys.listPages(wsId))?.pages[0]
        ?.nextCursor,
    ).toBe("cursor-1");
  });

  it("refreshes the unread summary the dropped rows can change", async () => {
    // Parity with web (packages/core/inbox/ws-updaters.ts). The tab badge
    // reads the server summary, and `issue:deleted` fires no `inbox:*` event,
    // so without this the badge stays lit over an emptied inbox (MUL-6967).
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    seed(qc);

    await dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
  });
});

describe("issue events during a next-page request", () => {
  // A next page appends to the pages it read when it started, so a patch that
  // lands meanwhile would be undone when the page arrives. The pages are
  // re-read instead.
  it.each([
    ["a status change", (qc: QueryClient) => patchInboxIssueStatus(qc, wsId, "issue-a", "done")],
    ["a deletion", (qc: QueryClient) => void dropInboxItemsByIssue(qc, wsId, "issue-a")],
  ])("keeps %s that lands before the page", async (_name, applyEvent) => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    let releaseNext!: (page: InboxPage) => void;
    const fetchPage = vi
      .fn<(cursor: string | null) => Promise<InboxPage>>()
      .mockResolvedValueOnce({ items: [item("n1", "issue-a")], nextCursor: "cursor-1", hasMore: true })
      .mockImplementationOnce(() => new Promise((resolve) => { releaseNext = resolve; }))
      .mockResolvedValue({ items: [], nextCursor: null, hasMore: false });
    const observer = new InfiniteQueryObserver(qc, {
      ...inboxPagesOptions(wsId),
      queryFn: ({ pageParam }) => fetchPage(pageParam),
    });
    const unsubscribe = observer.subscribe(() => {});
    await vi.waitFor(() => expect(observer.getCurrentResult().isSuccess).toBe(true));
    void observer.fetchNextPage();
    await vi.waitFor(() => expect(fetchPage).toHaveBeenCalledTimes(2));

    applyEvent(qc);
    releaseNext({ items: [item("n2", "issue-b")], nextCursor: null, hasMore: false });

    await vi.waitFor(() => expect(fetchPage).toHaveBeenCalledTimes(3));
    await vi.waitFor(() =>
      expect(qc.getQueryState(inboxKeys.listPages(wsId))?.fetchStatus).toBe("idle"));
    const rows = qc
      .getQueryData<InfiniteData<InboxPage>>(inboxKeys.listPages(wsId))
      ?.pages.flatMap((page) => page.items);
    expect(rows?.some((row) => row.issue_id === "issue-a" && row.issue_status !== "done")).toBe(false);
    expect(fetchPage.mock.calls.map(([cursor]) => cursor)).toEqual([null, "cursor-1", null]);
    unsubscribe();
  });

  it("leaves a running refetch alone", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    seed(qc);
    const cancel = vi.spyOn(qc, "cancelQueries");
    // Never settles; clearing the client below cancels it.
    void qc
      .fetchInfiniteQuery({
        ...inboxPagesOptions(wsId),
        queryFn: () => new Promise<InboxPage>(() => {}),
      })
      .catch(() => undefined);
    expect(qc.getQueryState(inboxKeys.listPages(wsId))?.fetchStatus).toBe("fetching");

    patchInboxIssueStatus(qc, wsId, "issue-a", "done");

    expect(cancel).not.toHaveBeenCalled();
    qc.clear();
  });
});

describe("refreshInboxUnreadSummary", () => {
  it("cancels an in-flight summary request BEFORE invalidating", async () => {
    // Parity with web. TanStack skips its own cancel on a FIRST fetch
    // (`Query.fetch` guards it on `state.data !== undefined`) and reuses the
    // request already on the wire, whose success clears `isInvalidated` — so
    // an invalidate alone can be answered by a pre-change response and never
    // asked again (MUL-6967).
    const qc = new QueryClient();
    const calls: string[] = [];
    vi.spyOn(qc, "cancelQueries").mockImplementation(async () => {
      calls.push("cancel");
    });
    vi.spyOn(qc, "invalidateQueries").mockImplementation(async () => {
      calls.push("invalidate");
    });

    await refreshInboxUnreadSummary(qc);

    expect(calls).toEqual(["cancel", "invalidate"]);
  });

  it("targets the summary key only, never a workspace inbox list", async () => {
    const qc = new QueryClient();
    const cancel = vi.spyOn(qc, "cancelQueries");

    await refreshInboxUnreadSummary(qc);

    expect(cancel).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
    expect(cancel).not.toHaveBeenCalledWith({ queryKey: inboxKeys.list(wsId) });
  });
});

describe("patchInboxIssueStatus", () => {
  it("updates the row's status without touching the badge", () => {
    // A status change moves no notification in or out of the unread set, so
    // it must NOT invalidate the summary — that would refetch on every
    // issue:updated frame.
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    seed(qc);

    patchInboxIssueStatus(qc, wsId, "issue-a", "done");

    const statuses = qc
      .getQueryData<InfiniteData<InboxPage>>(inboxKeys.listPages(wsId))
      ?.pages.map((page) => page.items.map((i) => i.issue_status));
    expect(statuses).toEqual([["done"], [null, "done"]]);
    expect(lookupItems(qc)?.[0]?.issue_status).toBe("done");
    expect(invalidate).not.toHaveBeenCalled();
  });

  it("leaves another workspace's inbox alone", () => {
    const qc = new QueryClient();
    const otherKey = inboxKeys.listLookup("workspace-2", "issue-a");
    qc.setQueryData<InboxPage>(otherKey, {
      items: [item("n9", "issue-a")],
      nextCursor: null,
      hasMore: false,
    });

    patchInboxIssueStatus(qc, wsId, "issue-a", "done");

    expect(qc.getQueryData<InboxPage>(otherKey)?.items[0]?.issue_status).toBe(null);
  });
});

describe("refreshInboxList", () => {
  it("cancels the in-flight list request BEFORE invalidating", async () => {
    // Parity with web. Without the cancel, a change during the list's first
    // load is answered by the pre-change request and never re-read — the tab
    // badge (already cancel-first) then counts rows the list does not show
    // (MUL-6967).
    const qc = new QueryClient();
    const calls: string[] = [];
    vi.spyOn(qc, "cancelQueries").mockImplementation(async () => {
      calls.push("cancel");
    });
    vi.spyOn(qc, "invalidateQueries").mockImplementation(async () => {
      calls.push("invalidate");
    });

    await refreshInboxList(qc, wsId);

    expect(calls).toEqual(["cancel", "invalidate"]);
  });

  it("targets this workspace's list, never the account-level summary", async () => {
    const qc = new QueryClient();
    const cancel = vi.spyOn(qc, "cancelQueries");

    await refreshInboxList(qc, wsId);

    expect(cancel).toHaveBeenCalledWith({ queryKey: inboxKeys.list(wsId) });
    expect(cancel).not.toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
  });

  it("reaches the paged list and every group lookup through the list prefix", async () => {
    const qc = new QueryClient();
    seed(qc);

    await refreshInboxList(qc, wsId);

    expect(qc.getQueryState(inboxKeys.listPages(wsId))?.isInvalidated).toBe(true);
    expect(
      qc.getQueryState(inboxKeys.listLookup(wsId, "issue-a"))?.isInvalidated,
    ).toBe(true);
    expect(qc.getQueryState(inboxKeys.unreadSummary())).toBeUndefined();
  });
});
