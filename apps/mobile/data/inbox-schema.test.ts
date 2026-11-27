import { describe, expect, it } from "vitest";
import { InboxListSchema, InboxUnreadSummarySchema } from "./schemas";

/**
 * Tests for mobile's CLIENT-SIDE parsing of GET /api/inbox. These are
 * hand-written fixtures run against `InboxListSchema`; they pin how this client
 * reacts to a payload and cannot fail when the Go server starts sending
 * something new.
 *
 * `details` is free-form JSON on the server and some producers write numbers
 * (children_done stores `total` and `stage` as ints). The endpoint parses an
 * ARRAY and `listInbox` falls back to `EMPTY_INBOX_LIST` on failure, so a
 * strict per-value type would blank the entire Inbox for one row. The schema
 * therefore normalizes values instead of rejecting them.
 */
describe("inbox list schema", () => {
  it("parses a row shaped like the documented server payload", () => {
    const serverRow = {
      id: "inbox-1",
      workspace_id: "ws-1",
      recipient_type: "member",
      recipient_id: "user-1",
      type: "status_changed",
      severity: "info",
      issue_id: "issue-1",
      title: "P0: delegated subscription rule",
      body: "",
      actor_type: "agent",
      actor_id: "agent-1",
      read: false,
      archived: false,
      created_at: "2026-07-30T00:00:00Z",
      details: { from: "in_progress", to: "in_review" },
    };

    const parsed = InboxListSchema.safeParse([serverRow]);
    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.type).toBe("status_changed");
    expect(parsed.success && parsed.data[0]?.details?.to).toBe("in_review");
  });

  it("stringifies numeric and boolean details values", () => {
    const row = {
      id: "inbox-2",
      recipient_type: "member",
      type: "children_done",
      details: { total: 3, stage: 0, all: true, rule: "child_done" },
    };

    const parsed = InboxListSchema.parse([row]);
    expect(parsed[0]?.details).toEqual({
      total: "3",
      stage: "0",
      all: "true",
      rule: "child_done",
    });
  });

  it("drops non-scalar details values instead of failing the row", () => {
    const row = {
      id: "inbox-3",
      recipient_type: "member",
      type: "children_done",
      details: { total: 2, stages: [{ stage: 1 }], extra: null, nested: { a: "b" } },
    };

    const parsed = InboxListSchema.parse([row]);
    expect(parsed[0]?.details).toEqual({ total: "2" });
  });

  it("reads a non-object details value as null", () => {
    const parsed = InboxListSchema.parse([
      { id: "inbox-4", recipient_type: "member", type: "status_changed", details: "oops" },
      { id: "inbox-5", recipient_type: "member", type: "status_changed" },
    ]);
    expect(parsed[0]?.details).toBeNull();
    expect(parsed[1]?.details).toBeNull();
  });

  it("keeps every row when one notification has numeric details", () => {
    // Regression for #11331: a children_done row with a numeric total used to
    // fail the array parse, and the whole Inbox rendered empty.
    const good = {
      id: "inbox-6",
      recipient_type: "member",
      type: "status_changed",
      details: { from: "todo", to: "in_review" },
    };
    const numeric = {
      id: "inbox-7",
      recipient_type: "member",
      type: "children_done",
      details: { total: 3 },
    };

    const parsed = InboxListSchema.parse([good, numeric]);
    expect(parsed.map((item) => item.id)).toEqual(["inbox-6", "inbox-7"]);
    expect(parsed[0]?.details?.to).toBe("in_review");
    expect(parsed[1]?.details?.total).toBe("3");
  });

  it("renders an unknown server type instead of dropping the row", () => {
    // Mirrors the root CLAUDE.md API-compatibility rule and mobile's own
    // "render every inbox type, never silently drop a category" parity rule: a
    // type this build has never heard of must still parse.
    const future = {
      id: "inbox-8",
      recipient_type: "member",
      type: "some_future_type",
      details: { anything: "still a string" },
    };

    const parsed = InboxListSchema.safeParse([future]);
    expect(parsed.success).toBe(true);
  });
});

/**
 * GET /api/inbox/unread-summary — the source of the inbox tab badge.
 *
 * Blast radius differs from the list above: `getInboxUnreadSummary` falls back
 * to an empty array, which reads as "nothing unread" and simply hides the
 * badge. A wrong number would be worse than no number, so the schema stays
 * strict about the shape and lenient only about extra fields.
 */
describe("inbox unread summary schema", () => {
  it("parses the documented server payload", () => {
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: 3 },
      { workspace_id: "ws-2", count: 1 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[1]?.count).toBe(1);
  });

  it("passes through a field this client does not know yet", () => {
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: 3, unread_mentions: 2 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.count).toBe(3);
  });

  it("reads a non-numeric count as zero rather than failing the list", () => {
    // One malformed row must not blank every other workspace's count.
    const parsed = InboxUnreadSummarySchema.safeParse([
      { workspace_id: "ws-1", count: "many" },
      { workspace_id: "ws-2", count: 5 },
    ]);

    expect(parsed.success).toBe(true);
    expect(parsed.success && parsed.data[0]?.count).toBe(0);
    expect(parsed.success && parsed.data[1]?.count).toBe(5);
  });

  it("rejects a row with no workspace id", () => {
    // Without an id the entry can never be matched to a workspace, so it would
    // silently contribute nothing — fail loudly into the empty fallback.
    expect(
      InboxUnreadSummarySchema.safeParse([{ count: 3 }]).success,
    ).toBe(false);
  });
});
