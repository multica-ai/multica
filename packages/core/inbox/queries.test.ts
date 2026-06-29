import { describe, expect, it } from "vitest";
import { deduplicateInboxItems } from "./queries";
import type { InboxItem } from "../types";

function item(overrides: Partial<InboxItem>): InboxItem {
  return {
    id: "item-1",
    workspace_id: "ws-1",
    recipient_type: "member",
    recipient_id: "user-1",
    actor_type: null,
    actor_id: null,
    type: "reminder",
    severity: "action_required",
    route: "inbox",
    issue_id: null,
    project_id: null,
    title: "Reminder",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-05-27T08:00:00Z",
    details: null,
    ...overrides,
  };
}

describe("deduplicateInboxItems", () => {
  it("keeps a fired reminder as its own standalone row, not folded into its issue group", () => {
    const firedReminder = item({
      id: "reminder",
      issue_id: "issue-1",
      muted_until: "2000-01-01T11:00:00Z", // resurfaced: due in the past
      created_at: "2000-01-01T08:00:00Z",
    });
    const newerComment = item({
      id: "comment",
      type: "new_comment",
      issue_id: "issue-1",
      muted_until: null,
      created_at: "2000-01-01T10:00:00Z",
    });
    const other = item({
      id: "other",
      issue_id: "issue-2",
      muted_until: null,
      created_at: "2000-01-01T10:30:00Z",
    });

    // The reminder is its own signal (keyed by id), so it does NOT swallow the
    // comment on the same issue; both survive, sorted by activation time.
    expect(
      deduplicateInboxItems([newerComment, firedReminder, other]).map(
        (i) => i.id,
      ),
    ).toEqual(["reminder", "other", "comment"]);
  });

  it("does not let a future muted reminder hide a newer active issue row", () => {
    const futureReminder = item({
      id: "reminder",
      issue_id: "issue-1",
      muted_until: "2999-01-01T11:00:00Z",
      created_at: "2000-01-01T08:00:00Z",
    });
    const activeComment = item({
      id: "comment",
      type: "new_comment",
      issue_id: "issue-1",
      muted_until: null,
      created_at: "2000-01-01T10:00:00Z",
    });

    expect(
      deduplicateInboxItems([futureReminder, activeComment]).map((i) => i.id),
    ).toEqual(["comment"]);
  });

  it("a READ fired reminder does not hide a newer unread comment on the same issue (FIR-2278)", () => {
    const readReminder = item({
      id: "reminder",
      issue_id: "issue-1",
      muted_until: null, // fired: due now
      read: true, // user already opened the reminder
      created_at: "2000-01-01T08:00:00Z",
    });
    const newerUnreadComment = item({
      id: "comment",
      type: "new_comment",
      issue_id: "issue-1",
      muted_until: null,
      read: false,
      created_at: "2000-01-01T10:00:00Z", // newer than the reminder
    });

    // The earlier bug: once the reminder was read it still won the per-issue
    // dedup and hid the newer comment. As a standalone row it no longer can —
    // both are present, and the unread comment is its issue's visible row.
    const ids = deduplicateInboxItems([readReminder, newerUnreadComment]).map(
      (i) => i.id,
    );
    expect(ids).toContain("comment");
    expect(ids).toContain("reminder");
  });
});
