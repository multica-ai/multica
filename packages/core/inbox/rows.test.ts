import { describe, expect, it } from "vitest";
import {
  inboxRowFromGroup,
  inboxRowHighlightCommentId,
  isGroupRow,
  type InboxRow,
} from "./rows";
import type { InboxGroup } from "../api/schemas";

function group(overrides: Partial<InboxGroup> = {}): InboxGroup {
  return {
    id: "group-1",
    workspace_id: "ws-1",
    recipient_id: "member-1",
    source_kind: "issue",
    source_id: "issue-1",
    unread: true,
    archived: false,
    seq: 4,
    state_version: 9,
    surfaced_at: "2026-06-15T08:00:00Z",
    event_id: "item-1",
    type: "new_comment",
    severity: "info",
    title: "Issue title",
    body: null,
    actor_type: "member",
    actor_id: "actor-1",
    details: { comment_id: "comment-4" },
    issue_id: "issue-1",
    issue_status: "in_progress",
    created_at: "2026-06-15T08:00:00Z",
    target_kind: "comment",
    target_id: "comment-4",
    ...overrides,
  } as InboxGroup;
}

describe("inboxRowFromGroup", () => {
  it("addresses the GROUP, not the representative event", () => {
    // Every write the page issues goes through `row.id`. Under v2 the thing
    // being marked read or archived is the group — pointing writes at the
    // event would move one event's booleans and leave the group, and therefore
    // every other client, untouched.
    const row = inboxRowFromGroup(group());
    expect(row.id).toBe("group-1");
    expect(row.id).not.toBe("item-1");
  });

  it("translates the cursor into the boolean the components read", () => {
    expect(inboxRowFromGroup(group({ unread: true })).read).toBe(false);
    expect(inboxRowFromGroup(group({ unread: false })).read).toBe(true);
  });

  it("carries the tokens a read has to report back", () => {
    const row = inboxRowFromGroup(group());
    expect(row.group?.seq).toBe(4);
    expect(row.group?.stateVersion).toBe(9);
    expect(isGroupRow(row)).toBe(true);
  });

  it("drops non-string details rather than rendering them", () => {
    // The mobile schema requires an all-strings map. A number here is what
    // blanks that client's entire inbox, so it is dropped at the boundary.
    const row = inboxRowFromGroup(
      group({ details: { comment_id: "c-1", failed_runs: 3 } as never }),
    );
    expect(row.details).toEqual({ comment_id: "c-1" });
  });

  it("survives a details value that is not an object at all", () => {
    expect(inboxRowFromGroup(group({ details: null })).details).toBeNull();
    expect(inboxRowFromGroup(group({ details: "nope" as never })).details).toBeNull();
  });
});

describe("inboxRowHighlightCommentId", () => {
  it("uses the server's resolved target for a group", () => {
    // details disagrees on purpose: v1 read that field, and reading it under
    // v2 would send the reader to the wrong comment.
    const row = inboxRowFromGroup(
      group({ target_id: "comment-9", details: { comment_id: "stale" } }),
    );
    expect(inboxRowHighlightCommentId(row)).toBe("comment-9");
  });

  it("has no target when the representative event is not a comment", () => {
    const row = inboxRowFromGroup(
      group({ target_kind: "issue", target_id: "issue-1", details: { comment_id: "c" } }),
    );
    expect(inboxRowHighlightCommentId(row)).toBeUndefined();
  });

  it("falls back to the details blob for a legacy row", () => {
    // v1 has no resolved target; the blob is all it ever had.
    const legacy = {
      id: "item-1",
      details: { comment_id: "comment-2" },
    } as unknown as InboxRow;
    expect(inboxRowHighlightCommentId(legacy)).toBe("comment-2");
    expect(isGroupRow(legacy)).toBe(false);
  });

  it("is undefined for nothing selected", () => {
    expect(inboxRowHighlightCommentId(null)).toBeUndefined();
    expect(inboxRowHighlightCommentId(undefined)).toBeUndefined();
  });
});
