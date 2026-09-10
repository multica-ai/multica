import { describe, it, expect, vi } from "vitest";
import { QueryClient } from "@tanstack/react-query";
import {
  onInboxInvalidate,
  onInboxIssueDeleted,
  onInboxIssueStatusChanged,
  onInboxSummaryInvalidate,
  patchInboxIssueProjection,
} from "./ws-updaters";
import { inboxKeys } from "./queries";
import type { InboxItem } from "../types";

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
  it("invalidates the workspace prefix, covering both the main and archived lists", () => {
    // Every inbox event can move an item across the two lists, so they are
    // always refreshed together (MUL-3736).
    const qc = new QueryClient();
    const spy = vi.spyOn(qc, "invalidateQueries");

    onInboxInvalidate(qc, wsId);

    expect(spy).toHaveBeenCalledWith({ queryKey: inboxKeys.all(wsId) });
  });

  it("does not reach the account-level summary key", () => {
    // The summary is keyed ["inbox", "unread-summary"], NOT under the
    // workspace prefix — its own updater owns it.
    const qc = new QueryClient();
    qc.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: wsId, count: 3 }]);
    const summaryQuery = qc
      .getQueryCache()
      .find({ queryKey: inboxKeys.unreadSummary() });

    onInboxInvalidate(qc, wsId);

    expect(summaryQuery?.state.isInvalidated).toBe(false);
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
