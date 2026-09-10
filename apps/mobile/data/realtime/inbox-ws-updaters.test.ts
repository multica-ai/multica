// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import type { InboxItem } from "@multica/core/types";
import { describe, expect, it, vi } from "vitest";

import { inboxKeys } from "@/data/queries/inbox";
import { dropInboxItemsByIssue, patchInboxIssueStatus } from "./inbox-ws-updaters";

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
  it("drops every row pointing at the deleted issue", () => {
    const qc = new QueryClient();
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [
      item("n1", "issue-a"),
      item("n2", "issue-b"),
    ]);

    dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(
      qc.getQueryData<InboxItem[]>(inboxKeys.list(wsId))?.map((i) => i.id),
    ).toEqual(["n2"]);
  });

  it("refreshes the unread summary the dropped rows can change", () => {
    // Parity with web (packages/core/inbox/ws-updaters.ts). The tab badge
    // reads the server summary, and `issue:deleted` fires no `inbox:*` event,
    // so without this the badge stays lit over an emptied inbox (MUL-6967).
    const qc = new QueryClient();
    const invalidate = vi.spyOn(qc, "invalidateQueries");
    qc.setQueryData<InboxItem[]>(inboxKeys.list(wsId), [item("n1", "issue-a")]);

    dropInboxItemsByIssue(qc, wsId, "issue-a");

    expect(invalidate).toHaveBeenCalledWith({
      queryKey: inboxKeys.unreadSummary(),
    });
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
