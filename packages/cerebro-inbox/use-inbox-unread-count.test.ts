import { describe, it, expect } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { countUnreadInboxConversations } from "./use-inbox-unread-count";

// FIR-2382 — the badge must never count more unread than the inbox list shows.
// These cases mirror the divergence Tine flagged: a channel whose only unread is
// a buried thread reply is HIDDEN in the classic inbox (channel.unread_count
// excludes threads server-side) but was still inflating the badge.
function inboxItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    id: over.id ?? "item",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "agent",
    actor_id: "a1",
    type: "dm_message",
    severity: "info",
    route: "inbox",
    issue_id: "chanA",
    project_id: null,
    title: "t",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-06-22T10:00:00Z",
    details: null,
    ...over,
  } as InboxItem;
}

describe("countUnreadInboxConversations (FIR-2382)", () => {
  it("counts an unread top-level channel message as one, zero once read", () => {
    const unread = [inboxItem({ id: "a", read: false })];
    expect(countUnreadInboxConversations(unread, { excludeThreadReplies: true })).toBe(1);
    const read = [inboxItem({ id: "a", read: true })];
    expect(countUnreadInboxConversations(read, { excludeThreadReplies: true })).toBe(0);
  });

  it("excludes an unread thread reply so a thread-only channel drops to zero", () => {
    // Top-level message already read, only a buried thread reply is unread.
    const items = [
      inboxItem({ id: "top", read: true, created_at: "2026-06-22T10:00:00Z" }),
      inboxItem({
        id: "reply",
        read: false,
        created_at: "2026-06-22T11:00:00Z", // newer, so it wins deduplication
        details: { comment_id: "c1", thread_root_id: "root1" },
      }),
    ];
    // Fixed: thread reply is dropped before dedup → channel counts 0 (matches hidden row).
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: true })).toBe(0);
    // Flag off (thread-split disabled) → legacy behaviour still counts it.
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: false })).toBe(1);
  });

  it("still counts a channel that has an unread top-level message plus a thread reply", () => {
    const items = [
      inboxItem({ id: "top", read: false, created_at: "2026-06-22T10:00:00Z" }),
      inboxItem({
        id: "reply",
        read: false,
        created_at: "2026-06-22T11:00:00Z",
        details: { comment_id: "c1", thread_root_id: "root1" },
      }),
    ];
    // The top-level unread survives the thread filter → one visible row, count 1.
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: true })).toBe(1);
  });

  it("deduplicates by issue: three unread notifications on one issue count once", () => {
    const items = [
      inboxItem({ id: "n1", issue_id: "iss1", type: "new_comment", read: false, created_at: "2026-06-22T10:00:00Z" }),
      inboxItem({ id: "n2", issue_id: "iss1", type: "new_comment", read: false, created_at: "2026-06-22T10:05:00Z" }),
      inboxItem({ id: "n3", issue_id: "iss1", type: "mentioned", read: false, created_at: "2026-06-22T10:10:00Z" }),
    ];
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: true })).toBe(1);
  });

  it("counts distinct unread conversations across issues", () => {
    const items = [
      inboxItem({ id: "a", issue_id: "iss1", read: false }),
      inboxItem({ id: "b", issue_id: "iss2", read: false }),
      inboxItem({ id: "c", issue_id: "iss3", read: true }),
    ];
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: true })).toBe(2);
  });

  it("ignores archived items", () => {
    const items = [
      inboxItem({ id: "a", issue_id: "iss1", read: false, archived: true }),
      inboxItem({ id: "b", issue_id: "iss2", read: false, archived: false }),
    ];
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: true })).toBe(1);
  });

  it("returns zero for an empty inbox", () => {
    expect(countUnreadInboxConversations([], { excludeThreadReplies: true })).toBe(0);
    expect(countUnreadInboxConversations([], { excludeThreadReplies: false })).toBe(0);
  });
});

describe("countUnreadInboxConversations with Round members", () => {
  it("counts Round-member issues through the normal inbox path", () => {
    const items = [
      inboxItem({ id: "a", issue_id: "round-issue", read: false }),
      inboxItem({ id: "b", issue_id: "plain-issue", read: false }),
    ];
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: false })).toBe(2);
  });

  it("keeps items without an issue id", () => {
    const items = [
      inboxItem({ id: "a", issue_id: null as unknown as string, read: false }),
      inboxItem({ id: "b", issue_id: "round-issue", read: false }),
    ];
    expect(countUnreadInboxConversations(items, { excludeThreadReplies: false })).toBe(2);
  });
});
