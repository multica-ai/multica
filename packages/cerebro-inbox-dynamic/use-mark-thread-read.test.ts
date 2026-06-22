import { describe, it, expect } from "vitest";
import type { InboxItem } from "@multica/core/types";
import { markThreadReadInInbox } from "./use-mark-thread-read";

// Mirrors the /api/inbox row shape: a channel/DM message carries issue_id (the
// channel) and, when it lives inside a thread, details.thread_root_id.
function row(over: Partial<InboxItem> & { id: string }): InboxItem {
  return {
    issue_id: "channelA",
    read: false,
    details: null,
    ...over,
  } as InboxItem;
}

describe("markThreadReadInInbox (FIR-1854 — reading a thread clears only that thread)", () => {
  const items: InboxItem[] = [
    // The channel's own top-level message — no thread_root_id.
    row({ id: "top", details: { comment_id: "c-top" } }),
    // Thread A: two unread rows for one reply (the dm_message + the mentioned dup).
    row({ id: "a1", details: { comment_id: "c-a1", thread_root_id: "rootA" } }),
    row({ id: "a2", details: { comment_id: "c-a2", thread_root_id: "rootA" } }),
    // Thread B in the same channel.
    row({ id: "b1", details: { comment_id: "c-b1", thread_root_id: "rootB" } }),
    // The SAME root id but in a different channel — must not be touched.
    row({ id: "other", issue_id: "channelB", details: { comment_id: "c-x", thread_root_id: "rootA" } }),
  ];

  const read = (out: InboxItem[] | undefined, id: string) =>
    out?.find((i) => (i as InboxItem & { id: string }).id === id)?.read;

  it("marks every row of the opened thread read", () => {
    const out = markThreadReadInInbox(items, "channelA", "rootA");
    expect(read(out, "a1")).toBe(true);
    expect(read(out, "a2")).toBe(true);
  });

  it("leaves the channel's own message and other threads unread (no 'clears both')", () => {
    const out = markThreadReadInInbox(items, "channelA", "rootA");
    expect(read(out, "top")).toBe(false); // channel top-level stays unread
    expect(read(out, "b1")).toBe(false); // sibling thread stays unread
    expect(read(out, "other")).toBe(false); // same root, different channel — untouched
  });

  it("does not crash on an empty/undefined cache", () => {
    expect(markThreadReadInInbox(undefined, "channelA", "rootA")).toBeUndefined();
    expect(markThreadReadInInbox([], "channelA", "rootA")).toEqual([]);
  });
});
