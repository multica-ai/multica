// @vitest-environment node
import { describe, it, expect, vi } from "vitest";
import {
  InfiniteQueryObserver,
  QueryClient,
  type InfiniteData,
  type QueryKey,
} from "@tanstack/react-query";
import {
  cancelInboxLists,
  onInboxInvalidate,
  onInboxIssueDeleted,
  onInboxNew,
  onInboxIssueStatusChanged,
  onInboxSummaryInvalidate,
  patchInboxIssueProjection,
} from "./ws-updaters";
import { EMPTY_INBOX_FILTERS } from "./filter-store";
import {
  archivedInboxPagesOptions,
  inboxCacheItems,
  inboxKeys,
  inboxPagesOptions,
  type InboxCache,
} from "./queries";
import type { InboxItem, InboxPage } from "../types";

const wsId = "ws-1";

function makeItem(
  id: string,
  issueId: string | null,
  overrides: Partial<InboxItem> = {},
): InboxItem {
  return {
    id,
    workspace_id: wsId,
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: null,
    actor_id: null,
    type: "mentioned",
    severity: "info",
    issue_id: issueId,
    title: `item ${id}`,
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2025-01-01T00:00:00Z",
    details: null,
    ...overrides,
  };
}

const activePagesKey = inboxPagesOptions(wsId, EMPTY_INBOX_FILTERS).queryKey;
const archivedPagesKey = archivedInboxPagesOptions(wsId, EMPTY_INBOX_FILTERS).queryKey;

function pageOf(items: InboxItem[]): InboxPage {
  return { items, nextCursor: null, hasMore: false };
}

// A view's pages cache after its first load: one page holding every row.
function seedRows(qc: QueryClient, queryKey: QueryKey, items: InboxItem[]) {
  qc.setQueryData<InfiniteData<InboxPage>>(queryKey, {
    pages: [pageOf(items)],
    pageParams: [null],
  });
}

function rows(qc: QueryClient, queryKey: QueryKey): InboxItem[] | undefined {
  const data = qc.getQueryData<InboxCache>(queryKey);
  return data && inboxCacheItems(data);
}

// A single-page pages query whose requests the test controls.
function pagesQuery(queryKey: QueryKey, queryFn: () => Promise<InboxPage>) {
  return { queryKey, queryFn, initialPageParam: null, getNextPageParam: () => undefined };
}

describe("onInboxIssueDeleted", () => {
  it("removes all inbox items referencing the deleted issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a"),
      makeItem("i2", "issue-a"),
      makeItem("i3", "issue-b"),
      makeItem("i4", null),
    ];
    seedRows(qc, activePagesKey, items);

    onInboxIssueDeleted(qc, wsId, "issue-a");

    expect(rows(qc, activePagesKey)?.map((i) => i.id)).toEqual(["i3", "i4"]);
  });

  it("also strips the issue from the archived list", () => {
    // Deleting an issue removes its rows whether they were archived or not, so
    // leaving them in the archived cache would render a row that 404s on tap.
    const qc = new QueryClient();
    seedRows(qc, archivedPagesKey, [
      makeItem("a1", "issue-a", { archived: true }),
      makeItem("a2", "issue-b", { archived: true }),
    ]);

    onInboxIssueDeleted(qc, wsId, "issue-a");

    expect(rows(qc, archivedPagesKey)?.map((i) => i.id)).toEqual(["a2"]);
  });

  it("is a no-op when the inbox cache is empty", () => {
    const qc = new QueryClient();
    expect(() => onInboxIssueDeleted(qc, wsId, "issue-a")).not.toThrow();
    expect(qc.getQueryCache().findAll({ queryKey: inboxKeys.all(wsId) })).toEqual([]);
  });

  it("refreshes the unread summary, which the dropped rows can change", async () => {
    // The badge reads the server summary, not these lists (MUL-6967). Deletion
    // arrives as an `issue:*` event, so no `inbox:*` handler runs to refresh
    // it, and the summary query is staleTime: Infinity with no refetch on
    // focus — without this the badge stays lit over an empty inbox.
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    seedRows(qc, activePagesKey, [makeItem("i1", "issue-a", { read: false })]);

    await onInboxIssueDeleted(qc, wsId, "issue-a");

    expect(rows(qc, activePagesKey)).toEqual([]);
    expect(spy).toHaveBeenCalledWith({ queryKey: inboxKeys.unreadSummary() });
  });
});

describe("onInboxInvalidate", () => {
  it("invalidates the workspace prefix, covering both the main and archived lists", async () => {
    // Every inbox event can move an item across the two lists, so they are
    // always refreshed together (MUL-3736).
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");

    await onInboxInvalidate(qc, wsId);

    expect(spy).toHaveBeenCalledWith({ queryKey: inboxKeys.all(wsId) });
  });

  it("does not reach the account-level summary key", async () => {
    // The summary is keyed ["inbox", "unread-summary"], NOT under the
    // workspace prefix — its own updater owns it.
    const qc = new QueryClient();
    qc.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: wsId, count: 3 }]);
    const summaryQuery = qc
      .getQueryCache()
      .find({ queryKey: inboxKeys.unreadSummary() });

    await onInboxInvalidate(qc, wsId);

    expect(summaryQuery?.state.isInvalidated).toBe(false);
  });
});

describe("onInboxInvalidate ordering", () => {
  it("cancels the in-flight list request BEFORE invalidating", async () => {
    // Same reason as the summary: a change during the list's first load must
    // not be answered by the request already on the wire. See the MUL-6967
    // regression at the bottom of this file for the end-to-end sequence.
    const qc = new QueryClient();
    const calls: string[] = [];
    vi.spyOn(qc, "cancelQueries").mockImplementation(async () => {
      calls.push("cancel");
    });
    vi.spyOn(qc, "invalidateQueries").mockImplementation(async () => {
      calls.push("invalidate");
    });

    await onInboxInvalidate(qc, wsId);

    expect(calls).toEqual(["cancel", "invalidate"]);
  });
});

describe("onInboxSummaryInvalidate", () => {
  it("invalidates the account-level summary key regardless of active workspace", async () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");

    await onInboxSummaryInvalidate(qc);

    expect(spy).toHaveBeenCalledWith({ queryKey: inboxKeys.unreadSummary() });
  });

  it("cancels an in-flight summary request BEFORE invalidating", async () => {
    // Order is the whole point. TanStack skips its own cancel on a first fetch
    // (`Query.fetch` guards it on `state.data !== undefined`) and hands back
    // the request already on the wire, whose success then clears
    // `isInvalidated` — so invalidating without cancelling first can be
    // answered by a pre-change response and never asked again (MUL-6967).
    const qc = new QueryClient();
    const calls: string[] = [];
    vi.spyOn(qc, "cancelQueries").mockImplementation(async () => {
      calls.push("cancel");
    });
    vi.spyOn(qc, "invalidateQueries").mockImplementation(async () => {
      calls.push("invalidate");
    });

    await onInboxSummaryInvalidate(qc);

    expect(calls).toEqual(["cancel", "invalidate"]);
  });

  it("cancels only the summary key, never a workspace inbox list", async () => {
    // A list request in flight for the active workspace must survive: the
    // cancel is aimed at the account-level summary alone.
    const qc = new QueryClient();
    const cancel = vi.spyOn(qc, "cancelQueries");
    seedRows(qc, activePagesKey, [makeItem("i1", "issue-a")]);

    await onInboxSummaryInvalidate(qc);

    expect(cancel).toHaveBeenCalledWith({ queryKey: inboxKeys.unreadSummary() });
    expect(cancel).not.toHaveBeenCalledWith({ queryKey: inboxKeys.list(wsId) });
    // The list cache entry is untouched (different key); only the summary
    // query is marked stale.
    expect(rows(qc, activePagesKey)?.[0]?.id).toBe("i1");
  });
});

describe("onInboxIssueStatusChanged", () => {
  it("updates issue_status only for items referencing the issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a", { issue_status: "todo" }),
      makeItem("i2", "issue-b", { issue_status: "todo" }),
    ];
    seedRows(qc, activePagesKey, items);

    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    const after = rows(qc, activePagesKey);
    expect(after?.find((i) => i.id === "i1")?.issue_status).toBe("done");
    expect(after?.find((i) => i.id === "i2")?.issue_status).toBe("todo");
  });

  it("patches archived rows too, which render the same status icon", () => {
    const qc = new QueryClient();
    seedRows(qc, archivedPagesKey, [
      makeItem("a1", "issue-a", { archived: true, issue_status: "todo" }),
    ]);

    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    expect(rows(qc, archivedPagesKey)?.[0]?.issue_status).toBe("done");
  });
});

describe("patchInboxIssueProjection", () => {
  it("patches priority in both active and archived issue rows", () => {
    const qc = new QueryClient();
    seedRows(qc, activePagesKey, [
      makeItem("i1", "issue-a", { issue_priority: "low" }),
      makeItem("i2", "issue-b", { issue_priority: "low" }),
    ]);
    seedRows(qc, archivedPagesKey, [
      makeItem("a1", "issue-a", {
        archived: true,
        issue_priority: "low",
      }),
    ]);

    patchInboxIssueProjection(qc, wsId, "issue-a", { priority: "urgent" });

    expect(
      rows(qc, activePagesKey)?.map((item) => item.issue_priority),
    ).toEqual(["urgent", "low"]);
    expect(rows(qc, archivedPagesKey)?.[0]?.issue_priority).toBe("urgent");
  });
});

// The hook-level reproduction lives in use-realtime-sync-inbox.test.tsx.
// These cases cover the partial writers and both views' pages, including an
// update to an issue absent from the cached rows (setQueryData's no-op trap).
describe.each([
  ["main", activePagesKey],
  ["archived", archivedPagesKey],
] as const)("partial updates to an invalidated %s inbox", (view, queryKey) => {
  it.each([
    ["status", (qc: QueryClient) => onInboxIssueStatusChanged(qc, wsId, "issue-a", "done")],
    ["priority", (qc: QueryClient) => patchInboxIssueProjection(qc, wsId, "issue-a", { priority: "high" })],
    ["unrelated issue", (qc: QueryClient) => onInboxIssueStatusChanged(qc, wsId, "other-issue", "done")],
    ["deletion", (qc: QueryClient) => onInboxIssueDeleted(qc, wsId, "issue-a")],
  ] as const)("preserves the pending refresh after %s", async (_label, update) => {
    const qc = new QueryClient();
    seedRows(qc, queryKey, [
      makeItem("i1", "issue-a", {
        issue_priority: "low",
        archived: view === "archived",
      }),
    ]);
    try {
      await onInboxInvalidate(qc, wsId);
      await update(qc);
      expect(qc.getQueryState(queryKey)?.isInvalidated).toBe(true);
    } finally {
      qc.clear();
    }
  });
});

it("does not refetch fresh lists for issue projection changes", async () => {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity } } });
  const queryFn = vi.fn(async () => pageOf([makeItem("i1", "issue-a")]));
  const options = pagesQuery(activePagesKey, queryFn);
  await qc.fetchInfiniteQuery(options);
  const observer = new InfiniteQueryObserver(qc, options);
  const unsubscribe = observer.subscribe(() => {});
  try {
    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");
    expect(
      observer.getCurrentResult().data?.pages[0]?.items[0]?.issue_status,
    ).toBe("done");
    expect(queryFn).toHaveBeenCalledTimes(1);
    expect(qc.getQueryState(options.queryKey)?.isInvalidated).toBe(false);
  } finally {
    unsubscribe();
    qc.clear();
  }
});

// The mutation-level regression (the write re-reads what it interrupted) lives
// in issues/mutations.test.tsx.
describe.each([
  ["main", activePagesKey],
  ["archived", archivedPagesKey],
] as const)("cancelInboxLists on the %s inbox", (_view, queryKey) => {
  it("reports and cancels only a request that is in flight", async () => {
    const qc = new QueryClient();
    const loaded = pageOf([makeItem("i1", "issue-a")]);
    const settled = { pages: [loaded], pageParams: [null] };
    let release!: (page: InboxPage) => void;
    const queryFn = vi
      .fn<() => Promise<InboxPage>>()
      .mockResolvedValueOnce(loaded)
      .mockImplementationOnce(
        () => new Promise((resolve) => (release = resolve)),
      );
    const options = pagesQuery(queryKey, queryFn);
    try {
      await qc.fetchInfiniteQuery(options);
      expect(cancelInboxLists(qc, wsId)).toBe(false);

      const refetch = qc.fetchInfiniteQuery(options);
      expect(cancelInboxLists(qc, wsId)).toBe(true);
      expect(qc.getQueryState(queryKey)?.fetchStatus).toBe("idle");
      release(pageOf([makeItem("i2", "issue-b"), ...loaded.items]));
      await expect(refetch).resolves.toEqual(settled);
      expect(qc.getQueryData(queryKey)).toEqual(settled);
    } finally {
      qc.clear();
    }
  });
});

// MUL-6967 regression: the inbox list must pick up a change that happens while
// its FIRST load is still in flight. Plain invalidation cannot do that —
// TanStack only cancels an in-flight request on invalidation once the query
// holds data, and otherwise answers the refetch with the request already on
// the wire. That pre-change response then clears `isInvalidated`, and with
// `staleTime: Infinity` nothing asks again, so the list stays behind while the
// badge (whose refresh already cancels first) moves on — "2 unread" over a list
// with nothing unread. The first load is not rare: nothing outside the Inbox
// page observes the list, so on web every visit after the cache is collected
// is a first load again.
describe("inbox list refresh during the first load", () => {
  it.each([
    ["new notification", (qc: QueryClient) => onInboxNew(qc, wsId, makeItem("n2", "issue-b"))],
    ["read / archive event", (qc: QueryClient) => onInboxInvalidate(qc, wsId)],
  ])("re-reads the list after a %s arrives mid-load", async (_label, signal) => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    let releaseFirst!: (page: InboxPage) => void;
    const firstResponse = new Promise<InboxPage>((resolve) => {
      releaseFirst = resolve;
    });
    let server: InboxItem[] = [makeItem("n1", "issue-a", { read: true })];
    const queryFn = vi
      .fn<() => Promise<InboxPage>>()
      .mockImplementationOnce(() => firstResponse)
      .mockImplementation(async () => pageOf(server));
    const observer = new InfiniteQueryObserver(qc, pagesQuery(activePagesKey, queryFn));
    const unsubscribe = observer.subscribe(() => {});

    try {
      expect(queryFn).toHaveBeenCalledTimes(1);
      // The server changes while the first read is still on the wire...
      server = [makeItem("n2", "issue-b", { read: false }), ...server];
      await signal(qc);
      // ...and only then does the pre-change response land.
      releaseFirst(pageOf([makeItem("n1", "issue-a", { read: true })]));

      await vi.waitFor(() =>
        expect(rows(qc, activePagesKey)?.map((i) => i.id)).toEqual(["n2", "n1"]),
      );
      expect(queryFn.mock.calls.length).toBeGreaterThan(1);
    } finally {
      releaseFirst(pageOf([]));
      unsubscribe();
      qc.clear();
    }
  });
});
