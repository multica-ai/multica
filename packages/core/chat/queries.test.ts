import { describe, expect, it } from "vitest";

import type { TaskMessagePayload } from "../types/events";
import type { ChatSession } from "../types/chat";
import {
  countUnreadChatSessions,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  sortChatSessions,
  taskMessagesOptions,
} from "./queries";

const msg = (seq: number): TaskMessagePayload => ({
  task_id: "task-1",
  issue_id: "issue-1",
  seq,
  type: "text",
  content: `m${seq}`,
});

describe("taskMessagesOptions", () => {
  it("fetches task messages for persisted UUID task ids", () => {
    const taskId = "4a2e8d1c-7f9b-4e2a-9c1d-123456789abc";

    expect(isTaskMessageTaskId(taskId)).toBe(true);
    expect(taskMessagesOptions(taskId).enabled).toBe(true);
  });

  it("does not fetch task messages for optimistic task ids", () => {
    const taskId = "optimistic-optimistic-1778739487737";

    expect(isTaskMessageTaskId(taskId)).toBe(false);
    expect(taskMessagesOptions(taskId).enabled).toBe(false);
  });
});

describe("mergeTaskMessagesBySeq", () => {
  it("backfills missing seqs and keeps the list seq-ordered", () => {
    const existing = [msg(1), msg(3)];
    const merged = mergeTaskMessagesBySeq(existing, [msg(2), msg(4)]);

    expect(merged.map((m) => m.seq)).toEqual([1, 2, 3, 4]);
  });

  it("drops duplicate seqs and lets the existing entry win", () => {
    const existing = [{ ...msg(1), content: "ws" }];
    const merged = mergeTaskMessagesBySeq(existing, [
      { ...msg(1), content: "refetch" },
      msg(2),
    ]);

    expect(merged.map((m) => m.seq)).toEqual([1, 2]);
    expect(merged.find((m) => m.seq === 1)?.content).toBe("ws");
  });

  it("preserves the array reference when nothing new arrives", () => {
    const existing = [msg(1), msg(2)];

    // Empty incoming and fully-duplicate incoming must both no-op so React
    // Query observers don't re-render on replayed events.
    expect(mergeTaskMessagesBySeq(existing, [])).toBe(existing);
    expect(mergeTaskMessagesBySeq(existing, [msg(1), msg(2)])).toBe(existing);
  });
});

describe("sortChatSessions", () => {
  const session = (over: Partial<ChatSession>): ChatSession => ({
    id: "s",
    workspace_id: "w",
    agent_id: "a",
    creator_id: "c",
    title: "t",
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  });

  it("puts pinned sessions before unpinned ones regardless of activity", () => {
    const pinnedOld = session({ id: "pinned-old", pinned: true, updated_at: "2026-01-01T00:00:00Z" });
    const unpinnedNew = session({ id: "unpinned-new", pinned: false, updated_at: "2026-06-01T00:00:00Z" });

    const sorted = sortChatSessions([unpinnedNew, pinnedOld]);
    expect(sorted.map((s) => s.id)).toEqual(["pinned-old", "unpinned-new"]);
  });

  it("orders within each group by most-recent activity (last message wins over updated_at)", () => {
    const a = session({
      id: "a",
      updated_at: "2026-01-01T00:00:00Z",
      last_message: { content: "x", role: "assistant", created_at: "2026-06-02T00:00:00Z" },
    });
    const b = session({ id: "b", updated_at: "2026-06-01T00:00:00Z" });

    const sorted = sortChatSessions([b, a]);
    expect(sorted.map((s) => s.id)).toEqual(["a", "b"]);
  });

  it("does not mutate the input array", () => {
    const input = [session({ id: "1" }), session({ id: "2", pinned: true })];
    const snapshot = input.map((s) => s.id);
    sortChatSessions(input);
    expect(input.map((s) => s.id)).toEqual(snapshot);
  });
});

describe("countUnreadChatSessions", () => {
  const session = (over: Partial<ChatSession>): ChatSession => ({
    id: "s",
    workspace_id: "w",
    agent_id: "a",
    creator_id: "c",
    title: "t",
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  });

  it("counts only active sessions that have unread", () => {
    const sessions = [
      session({ id: "active-unread", status: "active", has_unread: true }),
      session({ id: "active-read", status: "active", has_unread: false }),
    ];

    expect(countUnreadChatSessions(sessions)).toBe(1);
  });

  it("excludes archived sessions even when they carry unread", () => {
    // The stuck-badge bug: an archived session keeps has_unread, but it is
    // hidden from the default list and read-only, so the badge must ignore it
    // (MUL-4372).
    const sessions = [
      session({ id: "archived-unread", status: "archived", has_unread: true }),
      session({ id: "active-unread", status: "active", has_unread: true }),
    ];

    expect(countUnreadChatSessions(sessions)).toBe(1);
  });

  it("returns 0 when the only unread sessions are archived", () => {
    const sessions = [
      session({ id: "archived-1", status: "archived", has_unread: true }),
      session({ id: "archived-2", status: "archived", has_unread: true }),
    ];

    expect(countUnreadChatSessions(sessions)).toBe(0);
  });
});
