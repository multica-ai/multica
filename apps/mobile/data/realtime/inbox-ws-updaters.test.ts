// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import { describe, expect, it, vi } from "vitest";

import { inboxKeys } from "@/data/queries/inbox";
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

describe("dropInboxItemsByIssue", () => {
  it("drops every row pointing at the deleted issue", async () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      item("n1", "issue-a"),
      item("n2", "issue-b"),
    ]);

    await dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id),
    ).toEqual(["n2"]);
  });

  it("refreshes the unread summary the dropped rows can change", async () => {
    // Parity with web (packages/core/inbox/ws-updaters.ts). The tab badge
    // reads the server summary, and `issue:deleted` fires no `inbox:*` event,
    // so without this the badge stays lit over an emptied inbox (MUL-6967).
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [item("n1", "issue-a")]);

    await dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
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
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [item("n1", "issue-a")]);

    patchInboxIssueStatus(qc, wsId, "issue-a", "done");

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.[0]?.issue_status,
    ).toBe("done");
    expect(invalidate).not.toHaveBeenCalled();
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
});
