// @vitest-environment node
import { afterEach, beforeEach, describe, it, expect, vi } from "vitest";
import { focusManager, InfiniteQueryObserver, QueryClient, QueryObserver, type InfiniteData } from "@tanstack/react-query";
import {
  cancelInboxLists,
  onInboxInvalidate,
  onInboxIssueDeleted,
  onInboxIssuesDeleted,
  onInboxNew,
  onInboxNewSummaryInvalidate,
  onInboxIssueStatusChanged,
  onInboxSummaryInvalidate,
  patchInboxIssueProjection,
} from "./ws-updaters";
import { setApiInstance } from "../api";
import { createQueryClient } from "../query-client";
import type { ApiClient } from "../api/client";
import { inboxIssueFieldsSupport, inboxPriorityFilterSupport } from "./filters";
import { INBOX_REFRESH_INTERVAL_MS } from "./incremental";
import {
  deduplicateInboxItems,
  inboxKeys,
  inboxListOptions,
  inboxUnreadSummaryOptions,
  snoozedInboxOptions,
} from "./queries";
import type { ArchivedInboxPage, InboxItem, SnoozedInboxIssue } from "../types";

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

describe("onInboxIssueDeleted", () => {
  it("removes all inbox items referencing the deleted issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a"),
      makeItem("i2", "issue-a"),
      makeItem("i3", "issue-b"),
      makeItem("i4", null),
    ];
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), items);

    onInboxIssueDeleted(qc, wsId, "issue-a");

    const after = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    expect(after?.map((i) => i.id)).toEqual(["i3", "i4"]);
  });

  it("also strips the issue from the archived list", () => {
    // Deleting an issue removes its rows whether they were archived or not, so
    // leaving them in the archived cache would render a row that 404s on tap.
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), [
      makeItem("a1", "issue-a", { archived: true }),
      makeItem("a2", "issue-b", { archived: true }),
    ]);

    onInboxIssueDeleted(qc, wsId, "issue-a");

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId))?.map((i) => i.id),
    ).toEqual(["a2"]);
  });

  it("is a no-op when the inbox cache is empty", () => {
    const qc = new QueryClient();
    expect(() => onInboxIssueDeleted(qc, wsId, "issue-a")).not.toThrow();
    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))).toBeUndefined();
  });

  it("refreshes the unread summary, which the dropped rows can change", async () => {
    // The badge reads the server summary, not these lists (MUL-6967). Deletion
    // arrives as an `issue:*` event, so no `inbox:*` handler runs to refresh
    // it, and the summary query is staleTime: Infinity with no refetch on
    // focus — without this the badge stays lit over an empty inbox.
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      makeItem("i1", "issue-a", { read: false }),
    ]);

    await onInboxIssueDeleted(qc, wsId, "issue-a");

    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))).toEqual([]);
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
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [makeItem("i1", "issue-a")]);

    await onInboxSummaryInvalidate(qc);

    expect(cancel).toHaveBeenCalledWith({ queryKey: inboxKeys.unreadSummary() });
    expect(cancel).not.toHaveBeenCalledWith({ queryKey: inboxKeys.list(wsId) });
    // The list cache entry is untouched (different key); only the summary
    // query is marked stale.
    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.[0]?.id).toBe("i1");
  });
});

describe("onInboxIssueStatusChanged", () => {
  it("updates issue_status only for items referencing the issue", () => {
    const qc = new QueryClient();
    const items = [
      makeItem("i1", "issue-a", { issue_status: "todo" }),
      makeItem("i2", "issue-b", { issue_status: "todo" }),
    ];
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), items);

    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    const after = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    expect(after?.find((i) => i.id === "i1")?.issue_status).toBe("done");
    expect(after?.find((i) => i.id === "i2")?.issue_status).toBe("todo");
  });

  it("patches archived rows too, which render the same status icon", () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), [
      makeItem("a1", "issue-a", { archived: true, issue_status: "todo" }),
    ]);

    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId))?.[0]?.issue_status,
    ).toBe("done");
  });
});

describe("patchInboxIssueProjection", () => {
  it("patches priority in both active and archived issue rows", () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      makeItem("i1", "issue-a", { issue_priority: "low" }),
      makeItem("i2", "issue-b", { issue_priority: "low" }),
    ]);
    qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), [
      makeItem("a1", "issue-a", {
        archived: true,
        issue_priority: "low",
      }),
    ]);

    patchInboxIssueProjection(qc, wsId, "issue-a", { priority: "urgent" });

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((item) =>
        item.issue_priority,
      ),
    ).toEqual(["urgent", "low"]);
    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId))?.[0]
        ?.issue_priority,
    ).toBe("urgent");
  });

  it("does not manufacture priority capability for legacy Inbox rows", () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      makeItem("legacy", "issue-a"),
    ]);

    patchInboxIssueProjection(qc, wsId, "issue-a", { priority: "urgent" });

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.[0],
    ).not.toHaveProperty("issue_priority");
  });
});

// The hook-level reproduction lives in use-realtime-sync-inbox.test.tsx.
// These cases cover the partial writers and both list keys, including an
// update to an issue absent from the cached list (setQueryData's no-op trap).
describe.each([
  ["main", inboxKeys.list(wsId)],
  ["archived", inboxKeys.archived(wsId)],
] as const)("partial updates to an invalidated %s inbox", (view, queryKey) => {
  it.each([
    ["status", (qc: QueryClient) => onInboxIssueStatusChanged(qc, wsId, "issue-a", "done")],
    ["priority", (qc: QueryClient) => patchInboxIssueProjection(qc, wsId, "issue-a", { priority: "high" })],
    ["unrelated issue", (qc: QueryClient) => onInboxIssueStatusChanged(qc, wsId, "other-issue", "done")],
    ["deletion", (qc: QueryClient) => onInboxIssueDeleted(qc, wsId, "issue-a")],
  ] as const)("preserves the pending refresh after %s", async (_label, update) => {
    const qc = new QueryClient();
    qc.setQueryData(queryKey, [
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
  const queryFn = vi.fn(async () => [makeItem("i1", "issue-a")]);
  const options = { queryKey: inboxKeys.list(wsId), queryFn };
  await qc.fetchQuery(options);
  const observer = new QueryObserver(qc, options);
  const unsubscribe = observer.subscribe(() => {});
  try {
    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");
    expect(observer.getCurrentResult().data?.[0]?.issue_status).toBe("done");
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
  ["main", inboxKeys.list(wsId)],
  ["archived", inboxKeys.archived(wsId)],
] as const)("cancelInboxLists on the %s inbox", (_view, queryKey) => {
  it("reports and cancels only a request that is in flight", async () => {
    const qc = new QueryClient();
    const loaded = [makeItem("i1", "issue-a")];
    let release!: (items: InboxItem[]) => void;
    const queryFn = vi
      .fn<() => Promise<InboxItem[]>>()
      .mockResolvedValueOnce(loaded)
      .mockImplementationOnce(
        () => new Promise((resolve) => (release = resolve)),
      );
    try {
      await qc.fetchQuery({ queryKey, queryFn });
      expect(cancelInboxLists(qc, wsId)).toBe(false);

      const refetch = qc.fetchQuery({ queryKey, queryFn });
      expect(cancelInboxLists(qc, wsId)).toBe(true);
      expect(qc.getQueryState(queryKey)?.fetchStatus).toBe("idle");
      release([makeItem("i2", "issue-b"), ...loaded]);
      await expect(refetch).resolves.toEqual(loaded);
      expect(qc.getQueryData(queryKey)).toEqual(loaded);
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
    let releaseFirst!: (rows: InboxItem[]) => void;
    const firstResponse = new Promise<InboxItem[]>((resolve) => {
      releaseFirst = resolve;
    });
    let server: InboxItem[] = [makeItem("n1", "issue-a", { read: true })];
    const queryFn = vi
      .fn<() => Promise<InboxItem[]>>()
      .mockImplementationOnce(() => firstResponse)
      .mockImplementation(async () => server);
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      expect(queryFn).toHaveBeenCalledTimes(1);
      // The server changes while the first read is still on the wire...
      server = [makeItem("n2", "issue-b", { read: false }), ...server];
      const signalled = signal(qc);
      // ...and only then does the pre-change response land.
      releaseFirst([makeItem("n1", "issue-a", { read: true })]);
      await signalled;

      await vi.waitFor(() =>
        expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id)).toEqual([
          "n2",
          "n1",
        ]),
      );
      expect(queryFn.mock.calls.length).toBeGreaterThan(1);
    } finally {
      releaseFirst([]);
      unsubscribe();
      qc.clear();
    }
  });
});

// The delete mutations call onInboxIssuesDeleted directly — their WS echo is a
// backstop, not the plan — so it owes the same discipline as the update
// writes: cancel before pruning, and re-read a list whose request the cancel
// interrupted (MUL-7286).
describe("onInboxIssuesDeleted", () => {
  it("removes all inbox items referencing any deleted issue, from both lists", async () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      makeItem("i1", "issue-a"),
      makeItem("i2", "issue-a"),
      makeItem("i3", "issue-b"),
      makeItem("i4", null),
    ]);
    qc.setQueryData<InboxItem[]>(inboxKeys.archived(wsId), [
      makeItem("a1", "issue-a", { archived: true }),
      makeItem("a2", "issue-b", { archived: true }),
    ]);

    await onInboxIssuesDeleted(qc, wsId, ["issue-a", "issue-b"]);

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id),
    ).toEqual(["i4"]);
    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.archived(wsId))?.map((i) => i.id),
    ).toEqual([]);
  });

  it("is a no-op without ids", async () => {
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    await onInboxIssuesDeleted(qc, wsId, []);

    expect(invalidate).not.toHaveBeenCalled();
  });

  it("refreshes the unread summary, which the dropped rows can change", async () => {
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      makeItem("i1", "issue-a", { read: false }),
    ]);

    await onInboxIssuesDeleted(qc, wsId, ["issue-a"]);

    expect(spy).toHaveBeenCalledWith({ queryKey: inboxKeys.unreadSummary() });
  });

  it("re-reads a list whose request the prune interrupted", async () => {
    // A pre-delete response landing after the prune would resurrect the
    // deleted rows; the cancel drops it, and the interrupted list is owed a
    // re-read whose response no longer contains them.
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    let releaseFirst!: (rows: InboxItem[]) => void;
    const firstResponse = new Promise<InboxItem[]>((resolve) => {
      releaseFirst = resolve;
    });
    const server: InboxItem[] = [makeItem("n2", "issue-c", { read: false })];
    const queryFn = vi
      .fn<() => Promise<InboxItem[]>>()
      .mockImplementationOnce(() => firstResponse)
      .mockImplementation(async () => server);
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      expect(queryFn).toHaveBeenCalledTimes(1);
      expect(qc.getQueryState(inboxKeys.list(wsId))?.fetchStatus).toBe("fetching");

      await onInboxIssuesDeleted(qc, wsId, ["issue-a"]);
      releaseFirst([makeItem("n1", "issue-a", { read: false })]);

      await vi.waitFor(() =>
        expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id)).toEqual(["n2"]),
      );
      expect(queryFn.mock.calls.length).toBeGreaterThan(1);
    } finally {
      releaseFirst([]);
      unsubscribe();
      qc.clear();
    }
  });

  it("adds no list request when none was interrupted", async () => {
    const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
    const queryFn = vi.fn(async () => [makeItem("i1", "issue-a")]);
    const options = { queryKey: inboxKeys.list(wsId), queryFn };
    await qc.fetchQuery(options);
    const observer = new QueryObserver(qc, options);
    const unsubscribe = observer.subscribe(() => {});
    const invalidate = vi.spyOn(qc, "invalidateQueries");

    try {
      await onInboxIssuesDeleted(qc, wsId, ["issue-a"]);

      expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id)).toEqual([]);
      expect(queryFn).toHaveBeenCalledTimes(1);
      expect(invalidate).not.toHaveBeenCalledWith({ queryKey: inboxKeys.all(wsId) });
    } finally {
      unsubscribe();
      qc.clear();
    }
  });
});

// What a listed row of an issue mirrors, and the inbox:new push does not carry.
const issueFields = {
  issue_identifier: "MUL-1",
  issue_status: "in_progress",
  issue_priority: "high",
  issue_project_id: "project-1",
  issue_assignee_type: "member",
  issue_assignee_id: "user-2",
  issue_due_date: null,
  issue_label_ids: ["label-1"],
} as const satisfies Partial<InboxItem>;

function listed(id: string, issueId: string | null, overrides: Partial<InboxItem> = {}): InboxItem {
  return makeItem(id, issueId, { ...issueFields, ...overrides });
}

function push(id: string, issueId: string | null, overrides: Partial<InboxItem> = {}): InboxItem {
  const { issue_status: _status, ...item } = makeItem(id, issueId, {
    created_at: "2026-11-24T10:00:00Z",
    ...overrides,
  });
  // The push carries issue_status only when its writer knew it.
  return ("issue_status" in overrides ? { ...item, issue_status: overrides.issue_status } : item) as InboxItem;
}

function inboxClient() {
  return new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
}

describe("onInboxNew", () => {
  it("writes the row into the loaded list, with its issue's fields, without a request", async () => {
    const qc = inboxClient();
    const older = listed("n1", "issue-a", { read: true, created_at: "2026-11-24T09:00:00Z" });
    const other = listed("n0", "issue-b", { issue_identifier: "MUL-2", created_at: "2026-11-24T09:30:00Z" });
    const queryFn = vi.fn(async () => [older, other]);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await onInboxNew(qc, wsId, push("n2", "issue-a"));

      expect(queryFn).toHaveBeenCalledTimes(1);
      const list = observer.getCurrentResult().data!;
      expect(list[0]).toEqual({ ...push("n2", "issue-a"), ...issueFields });
      // One row per issue, the newest notification on top.
      expect(deduplicateInboxItems(list).map((row) => [row.id, row.read])).toEqual([
        ["n2", false],
        ["n0", false],
      ]);
      // The filters still see a server that has the issue fields.
      expect(inboxIssueFieldsSupport(list)).toBe("supported");
      expect(inboxPriorityFilterSupport(list)).toBe("supported");
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("writes a row it already holds only once", async () => {
    const qc = inboxClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [listed("n1", "issue-a")]);

    await onInboxNew(qc, wsId, push("n2", "issue-a"));
    const once = qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId));
    await onInboxNew(qc, wsId, push("n2", "issue-a"));

    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))).toBe(once);
    expect(once?.map((row) => row.id)).toEqual(["n2", "n1"]);
  });

  it("shows the issue's current status when a notification arrives after the issue moved on", async () => {
    const qc = inboxClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [listed("n1", "issue-a")]);
    onInboxIssueStatusChanged(qc, wsId, "issue-a", "done");

    // Written while the issue was still in progress, delivered after.
    await onInboxNew(qc, wsId, push("n2", "issue-a", { issue_status: "in_progress" }));

    const [row] = deduplicateInboxItems(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))!);
    expect(row).toMatchObject({ id: "n2", issue_status: "done" });
  });

  it("brings an archived issue back: its archived rows go, the archive and Snoozed are marked stale but not re-read", async () => {
    const qc = inboxClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [listed("n1", "issue-b")]);
    const pagesKey = [...inboxKeys.pages(wsId), {}];
    const pagesFn = vi.fn(async (): Promise<ArchivedInboxPage> => ({ items: [], nextCursor: null, hasMore: false }));
    qc.setQueryData<InfiniteData<ArchivedInboxPage>>(pagesKey, {
      pages: [{
        items: [
          listed("a1", "issue-a", { archived: true, issue_identifier: "MUL-9" }),
          listed("a2", "issue-c", { archived: true }),
        ],
        nextCursor: null,
        hasMore: false,
      }],
      pageParams: [null],
    });
    const lookupKey = [...inboxKeys.lookup(wsId), "issue-a", {}];
    qc.setQueryData<ArchivedInboxPage>(lookupKey, {
      items: [listed("a1", "issue-a", { archived: true })],
      nextCursor: null,
      hasMore: false,
    });
    const facetsKey = [...inboxKeys.facets(wsId), {}];
    qc.setQueryData(facetsKey, { unreadCount: 0 });
    qc.setQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId), [
      { issue_id: "issue-z", item_id: "z1", snoozed_until: "2099-01-01T00:00:00Z", snoozed_at: "2026-11-24T00:00:00Z", updates: 0 },
    ]);
    // The Archived tab is open.
    const archive = new InfiniteQueryObserver(qc, {
      queryKey: pagesKey,
      queryFn: pagesFn,
      initialPageParam: null,
      getNextPageParam: () => undefined,
    });
    const unsubscribe = archive.subscribe(() => {});

    try {
      await onInboxNew(qc, wsId, push("n2", "issue-a"));

      // The issue fields come from the archived row of the same issue.
      expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.[0]).toMatchObject({
        id: "n2",
        issue_identifier: "MUL-9",
      });
      expect(archive.getCurrentResult().data?.pages[0]?.items.map((row) => row.id)).toEqual(["a2"]);
      expect(qc.getQueryData<ArchivedInboxPage>(lookupKey)?.items).toEqual([]);
      expect(pagesFn).not.toHaveBeenCalled();
      for (const key of [pagesKey, lookupKey, facetsKey, inboxKeys.snoozed(wsId)]) {
        expect(qc.getQueryState(key)?.isInvalidated).toBe(true);
      }
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("re-reads the lists for an issue it holds no row of", async () => {
    const qc = inboxClient();
    const server = [listed("n2", "issue-new"), listed("n1", "issue-a")];
    const queryFn = vi.fn().mockResolvedValueOnce([listed("n1", "issue-a")]).mockResolvedValue(server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await onInboxNew(qc, wsId, push("n2", "issue-new"));

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(observer.getCurrentResult().data).toEqual(server);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("re-reads the lists for a notification that ended a snooze", async () => {
    const qc = inboxClient();
    // The issue came back with its earlier rows, which the list never held.
    const server = [listed("n2", "issue-a"), listed("s1", "issue-a"), listed("n1", "issue-b")];
    const queryFn = vi.fn().mockResolvedValueOnce([listed("n1", "issue-b"), listed("old", "issue-a")]).mockResolvedValue(server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    qc.setQueryData<SnoozedInboxIssue[]>(inboxKeys.snoozed(wsId), [
      { issue_id: "issue-a", item_id: "s1", snoozed_until: "2099-01-01T00:00:00Z", snoozed_at: "2026-11-24T00:00:00Z", updates: 1 },
    ]);
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await onInboxNew(qc, wsId, push("n2", "issue-a"));

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(observer.getCurrentResult().data).toEqual(server);
      expect(qc.getQueryState(inboxKeys.snoozed(wsId))?.isInvalidated).toBe(true);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it.each([
    ["a reconnect's re-read", (qc: QueryClient) => void onInboxInvalidate(qc, wsId)],
    ["a retry", (qc: QueryClient) => void qc.refetchQueries({ queryKey: inboxKeys.list(wsId) })],
  ])("is not answered by %s already on the wire", async (_label, startRead) => {
    // Written locally while that read is out, the row would be overwritten by
    // the read's older answer, and nothing would ask again.
    const qc = inboxClient();
    let server = [listed("n1", "issue-a")];
    let releaseStale!: () => void;
    const queryFn = vi
      .fn<() => Promise<InboxItem[]>>()
      .mockImplementationOnce(async () => server)
      .mockImplementationOnce(() => {
        const answer = server;
        return new Promise((resolve) => (releaseStale = () => resolve(answer)));
      })
      .mockImplementation(async () => server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      startRead(qc);
      await vi.waitFor(() => expect(queryFn).toHaveBeenCalledTimes(2));
      server = [listed("n2", "issue-a"), ...server];
      const handled = onInboxNew(qc, wsId, push("n2", "issue-a"));
      releaseStale();
      await handled;

      await vi.waitFor(() =>
        expect(observer.getCurrentResult().data?.map((row) => row.id)).toEqual(["n2", "n1"]),
      );
      expect(observer.getCurrentResult().fetchStatus).toBe("idle");
    } finally {
      releaseStale?.();
      unsubscribe();
      qc.clear();
    }
  });

  it("re-reads the lists for an issue-less notice, which the list hides from a guest", async () => {
    const qc = inboxClient();
    // A guest's list leaves out everything outside a project.
    const server = [listed("n1", "issue-a")];
    const queryFn = vi.fn(async () => server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await onInboxNew(qc, wsId, push("joined", null, { type: "project_owner_joined" }));

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(observer.getCurrentResult().data?.map((row) => row.id)).toEqual(["n1"]);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("re-reads rather than guess the order of two notifications on one issue from the same second", async () => {
    const qc = inboxClient();
    const second = "2026-11-24T10:00:00Z";
    // The server holds both; the mention is newer within that second.
    const server = [
      listed("mention", "issue-a", { type: "mentioned", created_at: second }),
      listed("comment", "issue-a", { type: "new_comment", created_at: second }),
      listed("n1", "issue-a"),
    ];
    const queryFn = vi.fn().mockResolvedValueOnce([listed("n1", "issue-a")]).mockResolvedValue(server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      // The pushes arrive newest first.
      await onInboxNew(qc, wsId, push("mention", "issue-a", { type: "mentioned", created_at: second }));
      expect(queryFn).toHaveBeenCalledTimes(1);
      await onInboxNew(qc, wsId, push("comment", "issue-a", { type: "new_comment", created_at: second }));

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(deduplicateInboxItems(observer.getCurrentResult().data!).map((row) => row.id)).toEqual(["mention"]);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("leaves a list that owes a re-read to that re-read (MUL-7286)", async () => {
    const qc = inboxClient();
    const server = [listed("n2", "issue-a"), listed("n1", "issue-a"), listed("x", "issue-x")];
    const queryFn = vi.fn().mockResolvedValueOnce([listed("n1", "issue-a")]).mockResolvedValue(server);
    await qc.fetchQuery({ queryKey: inboxKeys.list(wsId), queryFn });
    // Marked by an event while nothing on screen reads the list.
    await qc.invalidateQueries({ queryKey: inboxKeys.list(wsId) });

    await onInboxNew(qc, wsId, push("n2", "issue-a"));

    expect(qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((row) => row.id)).toEqual(["n1"]);
    expect(qc.getQueryState(inboxKeys.list(wsId))?.isInvalidated).toBe(true);
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.list(wsId), queryFn });
    const unsubscribe = observer.subscribe(() => {});
    try {
      await vi.waitFor(() => expect(observer.getCurrentResult().data).toEqual(server));
    } finally {
      unsubscribe();
      qc.clear();
    }
  });
});

describe("onInboxNewSummaryInvalidate", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    focusManager.setFocused(undefined);
    vi.useRealTimers();
  });

  it("follows a summary first load with one read, one at a time, whatever arrives meanwhile", async () => {
    // The client's cancel does not stop the query the server started, so
    // cancelling the first load would put two reads on the server.
    const qc = inboxClient();
    let reading = 0;
    let peak = 0;
    const queryFn = vi.fn(() => {
      reading += 1;
      peak = Math.max(peak, reading);
      const calls = queryFn.mock.calls.length;
      return new Promise<{ workspace_id: string; count: number }[]>((resolve) =>
        setTimeout(() => {
          reading -= 1;
          resolve([{ workspace_id: wsId, count: calls }]);
        }, 250),
      );
    });
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.unreadSummary(), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await vi.advanceTimersByTimeAsync(10);
      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(10);
      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS * 5);

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(peak).toBe(1);
      expect(observer.getCurrentResult().data).toEqual([{ workspace_id: wsId, count: 2 }]);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it.each([
    ["before", 100],
    ["after", 1500],
  ])("is not held by a read that pauses its retry while the window is hidden (next push %s the pause)", async (_when, nextPushMs) => {
    // The app's own client and inbox retry policy: a 500 is retried once,
    // about a second later, and a hidden window holds that retry for focus.
    // The desktop dock badge keeps the summary enabled meanwhile.
    const qc = createQueryClient();
    const queryFn = vi
      .fn<() => Promise<{ workspace_id: string; count: number }[]>>()
      .mockResolvedValueOnce([{ workspace_id: wsId, count: 8 }])
      .mockRejectedValueOnce(Object.assign(new Error("500"), { status: 500 }))
      .mockResolvedValue([{ workspace_id: wsId, count: 9 }]);
    const observer = new QueryObserver(qc, { queryKey: inboxKeys.unreadSummary(), queryFn });
    const unsubscribe = observer.subscribe(() => {});

    try {
      await vi.advanceTimersByTimeAsync(10);
      focusManager.setFocused(false);
      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(nextPushMs);
      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS * 3);

      expect(queryFn).toHaveBeenCalledTimes(3);
      expect(observer.getCurrentResult().data).toEqual([{ workspace_id: wsId, count: 9 }]);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });

  it("does not wait on a retry paused while the window is hidden", async () => {
    // The desktop dock badge keeps the summary enabled while the window is
    // minimized, where TanStack holds a retry until focus returns.
    const qc = inboxClient();
    const queryFn = vi
      .fn<() => Promise<{ workspace_id: string; count: number }[]>>()
      .mockRejectedValueOnce(new Error("503"))
      .mockResolvedValue([{ workspace_id: wsId, count: 1 }]);
    const observer = new QueryObserver(qc, {
      queryKey: inboxKeys.unreadSummary(),
      queryFn,
      retry: 1,
      retryDelay: 0,
    });
    focusManager.setFocused(false);
    const unsubscribe = observer.subscribe(() => {});

    try {
      await vi.advanceTimersByTimeAsync(10);
      expect(qc.getQueryState(inboxKeys.unreadSummary())?.fetchStatus).toBe("paused");

      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(10);

      expect(queryFn).toHaveBeenCalledTimes(2);
      expect(observer.getCurrentResult().data).toEqual([{ workspace_id: wsId, count: 1 }]);
    } finally {
      unsubscribe();
      qc.clear();
    }
  });
});

// The cost this exists for: every open client re-read every inbox list and
// the unread summary on each inbox:new.
describe("inbox requests for a burst of inbox:new", () => {
  const N = 20;
  const LATENCY_MS = 50;

  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  async function burst(events: InboxItem[], spacingMs: number) {
    const respond = <T,>(value: T) => new Promise<T>((resolve) => setTimeout(() => resolve(value), LATENCY_MS));
    const api = {
      listInbox: vi.fn(() => respond([listed("n1", "issue-a"), listed("n0", "issue-b")])),
      listSnoozedInbox: vi.fn(() => respond([] as SnoozedInboxIssue[])),
      getInboxUnreadSummary: vi.fn(() => respond([{ workspace_id: wsId, count: 2 }])),
    };
    setApiInstance(api as unknown as ApiClient);
    const qc = inboxClient();
    // The Inbox page: the list, the snoozed issues its open row is checked
    // against, and the badge.
    const observers = [
      new QueryObserver(qc, inboxListOptions(wsId)),
      new QueryObserver(qc, snoozedInboxOptions(wsId)),
      new QueryObserver(qc, inboxUnreadSummaryOptions()),
    ];
    const unsubscribes = observers.map((observer) => observer.subscribe(() => {}));
    await vi.advanceTimersByTimeAsync(LATENCY_MS);
    for (const fn of Object.values(api)) fn.mockClear();

    for (const item of events) {
      // What handleInboxNew does with each.
      void onInboxNew(qc, wsId, item);
      void onInboxNewSummaryInvalidate(qc);
      await vi.advanceTimersByTimeAsync(spacingMs);
    }
    await vi.advanceTimersByTimeAsync(INBOX_REFRESH_INTERVAL_MS * 3);
    const requests = {
      list: api.listInbox.mock.calls.length,
      snoozed: api.listSnoozedInbox.mock.calls.length,
      summary: api.getInboxUnreadSummary.mock.calls.length,
    };
    for (const unsubscribe of unsubscribes) unsubscribe();
    qc.clear();
    return requests;
  }

  // Server times are to the second; each push here is a second apart unless
  // the scenario says otherwise.
  const at = (i: number) => new Date(Date.UTC(2026, 10, 24, 10, 0, i)).toISOString().replace(".000Z", "Z");
  const onKnownIssues = Array.from({ length: N }, (_, i) =>
    push(`k${i}`, i % 2 ? "issue-a" : "issue-b", { created_at: at(i) }),
  );
  const onKnownIssuesSameSecond = Array.from({ length: N }, (_, i) =>
    push(`s${i}`, i % 2 ? "issue-a" : "issue-b", { created_at: at(0) }),
  );
  const onNewIssues = Array.from({ length: N }, (_, i) => push(`u${i}`, `issue-new-${i}`, { created_at: at(i) }));

  it.each([
    // An agent at work: a notification every 100ms for 2s.
    ["on issues in the list, 100ms apart", onKnownIssues, 100, { list: 0, snoozed: 0, summary: 3 }],
    ["on issues in the list, all at once", onKnownIssues, 0, { list: 0, snoozed: 0, summary: 2 }],
    ["on issues in the list, all in one second", onKnownIssuesSameSecond, 100, { list: 3, snoozed: 3, summary: 3 }],
    ["each on a new issue, 100ms apart", onNewIssues, 100, { list: 3, snoozed: 3, summary: 3 }],
    ["each on a new issue, all at once", onNewIssues, 0, { list: 2, snoozed: 2, summary: 2 }],
  ])("%s", async (_label, events, spacingMs, expected) => {
    expect(await burst(events, spacingMs)).toEqual(expected);
  });
});
