import { describe, expect, it } from "vitest";
import type { InboxItem, InboxWorkspaceUnread } from "../types";
import { deduplicateInboxItems, hasOtherWorkspaceUnread, inboxKeys, unreadWorkspaceIds } from "./queries";

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
