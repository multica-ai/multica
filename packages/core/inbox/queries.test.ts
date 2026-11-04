import { describe, expect, it } from "vitest";
import { createQueryClient } from "../query-client";
import type { InboxItem, InboxWorkspaceUnread } from "../types";
import { EMPTY_INBOX_FILTERS, type InboxFilters } from "./filter-store";
import {
  archivedInboxFacetsOptions,
  archivedInboxPagesOptions,
  deduplicateArchivedInboxItems,
  deduplicateInboxItems,
  hasOtherWorkspaceUnread,
  inboxItemsReadOnIssueOpen,
  inboxKeys,
  issueOpenMayReadInbox,
  unreadCountForWorkspace,
  unreadWorkspaceIds,
} from "./queries";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-06-15T08:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("deduplicateInboxItems", () => {
  it("keeps the newest issue row while preserving an older comment anchor", () => {
    const merged = deduplicateInboxItems([
      item({
        id: "comment-notification",
        type: "new_comment",
        created_at: "2026-06-15T08:00:00Z",
        details: { comment_id: "comment-1" },
      }),
      item({
        id: "status-notification",
        type: "status_changed",
        created_at: "2026-06-15T08:01:00Z",
        details: { from: "in_progress", to: "in_review" },
      }),
    ]);

    expect(merged).toHaveLength(1);
    expect(merged[0]).toMatchObject({
      id: "status-notification",
      type: "status_changed",
      details: {
        from: "in_progress",
        to: "in_review",
        comment_id: "comment-1",
      },
    });
  });

  it("preserves the newest row's own comment anchor", () => {
    const merged = deduplicateInboxItems([
      item({
        id: "older-comment",
        created_at: "2026-06-15T08:00:00Z",
        details: { comment_id: "comment-1" },
      }),
      item({
        id: "newer-comment",
        created_at: "2026-06-15T08:02:00Z",
        details: { comment_id: "comment-2" },
      }),
    ]);

    expect(merged).toHaveLength(1);
    expect(merged[0]?.id).toBe("newer-comment");
    expect(merged[0]?.details?.comment_id).toBe("comment-2");
  });

  it("drops archived rows so an optimistic archive leaves the list at once", () => {
    const merged = deduplicateInboxItems([
      item({ id: "active", issue_id: "issue-1" }),
      item({ id: "filed-away", issue_id: "issue-2", archived: true }),
    ]);

    expect(merged.map((i) => i.id)).toEqual(["active"]);
  });
});

describe("deduplicateArchivedInboxItems", () => {
  it("keeps only archived rows, one per issue, newest first", () => {
    const merged = deduplicateArchivedInboxItems([
      item({
        id: "archived-older",
        issue_id: "issue-1",
        archived: true,
        created_at: "2026-06-15T08:00:00Z",
      }),
      item({
        id: "archived-newer",
        issue_id: "issue-1",
        archived: true,
        created_at: "2026-06-15T09:00:00Z",
      }),
      item({
        id: "archived-other-issue",
        issue_id: "issue-2",
        archived: true,
        created_at: "2026-06-15T07:00:00Z",
      }),
      item({ id: "still-active", issue_id: "issue-3" }),
    ]);

    expect(merged.map((i) => i.id)).toEqual([
      "archived-newer",
      "archived-other-issue",
    ]);
  });

  it("drops a row the moment an optimistic unarchive flips it back", () => {
    // What useUnarchiveInbox's onMutate does: flip `archived` on the archived
    // cache. The row must leave this list without waiting for the refetch.
    const restored = item({ id: "restored", archived: false });

    expect(deduplicateArchivedInboxItems([restored])).toEqual([]);
  });

  it("groups issue-less notifications on their own id rather than merging them", () => {
    const merged = deduplicateArchivedInboxItems([
      item({ id: "standalone-1", issue_id: null, archived: true }),
      item({ id: "standalone-2", issue_id: null, archived: true }),
    ]);

    expect(merged).toHaveLength(2);
  });
});

describe("archived inbox options", () => {
  it("keeps an idle selection out of archive query keys", () => {
    const idle: InboxFilters = { ...EMPTY_INBOX_FILTERS, idleOnly: true };

    expect(archivedInboxPagesOptions("ws", idle).queryKey).toEqual(
      archivedInboxPagesOptions("ws", EMPTY_INBOX_FILTERS).queryKey,
    );
    expect(archivedInboxFacetsOptions("ws", idle).queryKey).toEqual(
      archivedInboxFacetsOptions("ws", EMPTY_INBOX_FILTERS).queryKey,
    );
  });
});

describe("hasOtherWorkspaceUnread", () => {
  const summary = (entries: InboxWorkspaceUnread[]) => entries;

  it("is true when a workspace other than the active one has unread", () => {
    expect(
      hasOtherWorkspaceUnread(
        summary([{ workspace_id: "ws-2", count: 3 }]),
        "ws-1",
      ),
    ).toBe(true);
  });

  it("excludes the active workspace's own unread", () => {
    expect(
      hasOtherWorkspaceUnread(
        summary([{ workspace_id: "ws-1", count: 5 }]),
        "ws-1",
      ),
    ).toBe(false);
  });

  it("ignores other workspaces whose count is zero", () => {
    expect(
      hasOtherWorkspaceUnread(
        summary([{ workspace_id: "ws-2", count: 0 }]),
        "ws-1",
      ),
    ).toBe(false);
  });

  it("is true when at least one non-active workspace has unread", () => {
    expect(
      hasOtherWorkspaceUnread(
        summary([
          { workspace_id: "ws-1", count: 4 },
          { workspace_id: "ws-2", count: 1 },
        ]),
        "ws-1",
      ),
    ).toBe(true);
  });

  it("is false for an empty summary", () => {
    expect(hasOtherWorkspaceUnread([], "ws-1")).toBe(false);
  });

  it("counts every workspace as 'other' when there is no active workspace", () => {
    expect(
      hasOtherWorkspaceUnread(
        summary([{ workspace_id: "ws-1", count: 2 }]),
        null,
      ),
    ).toBe(true);
  });
});

describe("unreadWorkspaceIds", () => {
  it("collects only workspaces with a non-zero count", () => {
    const ids = unreadWorkspaceIds([
      { workspace_id: "ws-1", count: 0 },
      { workspace_id: "ws-2", count: 3 },
      { workspace_id: "ws-3", count: 1 },
    ]);
    expect(ids.has("ws-1")).toBe(false);
    expect(ids.has("ws-2")).toBe(true);
    expect(ids.has("ws-3")).toBe(true);
    expect(ids.size).toBe(2);
  });

  it("returns an empty set for an empty summary", () => {
    expect(unreadWorkspaceIds([]).size).toBe(0);
  });
});

describe("inboxKeys.unreadSummary", () => {
  it("is a stable account-level key independent of any workspace", () => {
    expect(inboxKeys.unreadSummary()).toEqual(["inbox", "unread-summary"]);
  });
});

// The inbox nav / tab / dock badges all read this instead of counting the
// inbox list, so that the count costs no list fetch (MUL-6967).
describe("unreadCountForWorkspace", () => {
  const summary: InboxWorkspaceUnread[] = [
    { workspace_id: "ws-1", count: 3 },
    { workspace_id: "ws-2", count: 7 },
  ];

  it("returns the requested workspace's count", () => {
    expect(unreadCountForWorkspace(summary, "ws-2")).toBe(7);
  });

  it("reads an absent workspace as zero", () => {
    // The endpoint omits workspaces with nothing unread rather than sending a
    // zero row, so "missing" must mean 0 and not "unknown".
    expect(unreadCountForWorkspace(summary, "ws-3")).toBe(0);
  });

  it("returns zero without a workspace", () => {
    expect(unreadCountForWorkspace(summary, null)).toBe(0);
    expect(unreadCountForWorkspace(summary, undefined)).toBe(0);
  });

  it("returns zero for an empty summary", () => {
    expect(unreadCountForWorkspace([], "ws-1")).toBe(0);
  });
});

describe("inboxItemsReadOnIssueOpen", () => {
  it("reads the issue's unread informational items only", () => {
    const ids = inboxItemsReadOnIssueOpen([
      item({ id: "comment" }),
      item({ id: "status", type: "status_changed" }),
      item({ id: "already-read", read: true }),
      item({ id: "archived", archived: true }),
      item({ id: "other-issue", issue_id: "issue-2" }),
    ], "issue-1");

    expect(ids).toEqual(["comment", "status"]);
  });

  it.each([
    ["issue_assigned", "action_required"],
    ["task_failed", "action_required"],
    ["autopilot_paused", "attention"],
    ["mentioned", "info"],
  ] as const)("reads nothing while %s is unread", (type, severity) => {
    const items = [item({ id: "kept", type, severity }), item({ id: "comment" })];

    expect(inboxItemsReadOnIssueOpen(items, "issue-1")).toEqual([]);
    expect(inboxItemsReadOnIssueOpen([{ ...items[0]!, read: true }, items[1]!], "issue-1")).toEqual(["comment"]);
  });
});

describe("issueOpenMayReadInbox", () => {
  it("answers from a current Inbox list", () => {
    const qc = createQueryClient();
    qc.setQueryData(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 3 }]);
    qc.setQueryData(inboxKeys.list("ws-1"), [item({ read: true })]);
    expect(issueOpenMayReadInbox(qc, "ws-1", "issue-1")).toBe(false);

    qc.setQueryData(inboxKeys.list("ws-1"), [item({})]);
    expect(issueOpenMayReadInbox(qc, "ws-1", "issue-1")).toBe(true);
  });

  it("falls back to the unread summary when the list is missing or stale", async () => {
    const qc = createQueryClient();
    qc.setQueryData(inboxKeys.list("ws-1"), [item({ read: true })]);
    await qc.invalidateQueries({ queryKey: inboxKeys.list("ws-1"), refetchType: "none" });
    qc.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), [{ workspace_id: "ws-1", count: 1 }]);
    expect(issueOpenMayReadInbox(qc, "ws-1", "issue-1")).toBe(true);

    qc.setQueryData<InboxWorkspaceUnread[]>(inboxKeys.unreadSummary(), [{ workspace_id: "ws-2", count: 1 }]);
    expect(issueOpenMayReadInbox(qc, "ws-1", "issue-1")).toBe(false);
  });

  it("asks the server when nothing is known", () => {
    expect(issueOpenMayReadInbox(createQueryClient(), "ws-1", "issue-1")).toBe(true);
  });
});
