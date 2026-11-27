// @vitest-environment node
import { QueryClient } from "@tanstack/react-query";
import { afterEach, describe, expect, it, vi } from "vitest";

import { setApiInstance } from "../api";
import type { ApiClient } from "../api/client";
import type { TaskMessagePayload } from "../types/events";
import type { ChatMessage, ChatSession } from "../types/chat";
import {
  chatKeys,
  countUnreadChatSessions,
  isTaskMessageTaskId,
  mergeTaskMessagesBySeq,
  prefetchChatSession,
  sortChatSessions,
  taskMessagesOptions,
  unionTaskMessagesBySeq,
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

describe("unionTaskMessagesBySeq", () => {
  it("keeps seqs the authoritative list never mentioned", () => {
    // A REST response snapshotted before seq 3 was persisted must not erase it.
    const united = unionTaskMessagesBySeq([msg(1), msg(3)], [msg(1), msg(2)]);

    expect(united.map((m) => m.seq)).toEqual([1, 2, 3]);
  });

  it("lets the authoritative row win on conflict", () => {
    const live = { ...msg(1), content: "live" };
    const persisted = { ...msg(1), content: "persisted" };

    const united = unionTaskMessagesBySeq([live], [persisted]);

    expect(united).toEqual([persisted]);
  });

  it("preserves the base reference when nothing differs", () => {
    // Identity is what keeps a duplicate event from re-rendering every
    // subscriber — AssistantMessage memoizes its timeline on this array.
    const base = [msg(1), msg(2)];

    expect(unionTaskMessagesBySeq(base, [base[0]!, base[1]!])).toBe(base);
    expect(unionTaskMessagesBySeq(base, [])).toBe(base);
  });

  it("sorts by seq regardless of arrival order", () => {
    expect(unionTaskMessagesBySeq([msg(5)], [msg(3), msg(9)]).map((m) => m.seq))
      .toEqual([3, 5, 9]);
  });

  it("handles an empty or absent base", () => {
    expect(unionTaskMessagesBySeq(undefined, [msg(2), msg(1)]).map((m) => m.seq))
      .toEqual([1, 2]);
    expect(unionTaskMessagesBySeq([], [msg(1)]).map((m) => m.seq)).toEqual([1]);
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

  // A restricted agent's tool call reaches a reader twice: stripped through
  // the workspace, whole through their own scope, in either order.
  describe("a tool call sent both with and without its details", () => {
    const hidden: TaskMessagePayload = { ...msg(2), type: "tool_use", tool: "Bash", content: undefined, details_hidden: true };
    const whole: TaskMessagePayload = { ...msg(2), type: "tool_use", tool: "Bash", content: undefined, input: { command: "env" } };

    it("keeps the whole call when it arrives second", () => {
      const merged = mergeTaskMessagesBySeq([msg(1), hidden], [whole]);
      expect(merged.find((m) => m.seq === 2)).toBe(whole);
    });

    it("keeps the whole call when the stripped one arrives second", () => {
      const existing = [msg(1), whole];
      expect(mergeTaskMessagesBySeq(existing, [hidden])).toBe(existing);
    });

    it("keeps the whole call when both land in one batch", () => {
      expect(mergeTaskMessagesBySeq([msg(1)], [whole, hidden]).find((m) => m.seq === 2)).toBe(whole);
      expect(mergeTaskMessagesBySeq([msg(1)], [hidden, whole]).find((m) => m.seq === 2)).toBe(whole);
    });
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

  it("preserves server order and session identities when activity times tie", () => {
    const first = session({ id: "first" });
    const second = session({ id: "second" });
    const pinned = session({ id: "pinned", pinned: true });

    const sorted = sortChatSessions([first, pinned, second]);
    expect(sorted).toEqual([pinned, first, second]);
    expect(sorted[1]).toBe(first);
    expect(sorted[2]).toBe(second);
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

describe("prefetchChatSession", () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  const makeMessage = (over: Partial<ChatMessage> & Pick<ChatMessage, "id">): ChatMessage => ({
    chat_session_id: "s-1",
    role: "assistant",
    content: "reply",
    task_id: null,
    created_at: "2026-01-01T00:00:00Z",
    ...over,
  });

  it("warms messages, pending task, wakeups, and up to two tail assistant timelines", async () => {
    const t1 = "11111111-1111-4111-8111-111111111111";
    const t2 = "22222222-2222-4222-8222-222222222222";
    const t3 = "33333333-3333-4333-8333-333333333333";

    const listMessages = vi.fn(async () => ({
      messages: [
        makeMessage({ id: "m1", role: "assistant", task_id: t1 }),
        makeMessage({ id: "m2", role: "user", task_id: t1 }),
        makeMessage({ id: "m3", role: "assistant", task_id: t2 }),
        makeMessage({ id: "m4", role: "assistant", task_id: "optimistic-1" }),
        makeMessage({ id: "m5", role: "assistant", task_id: t3 }),
      ],
      limit: 50,
      has_more: false,
      next_cursor: null,
    }));
    const getPending = vi.fn(async (_sessionId: string) => ({}));
    const listWakeups = vi.fn(async (_sessionId: string) => []);
    const listTaskMessages = vi.fn(async (_taskId: string) => []);
    setApiInstance({
      listChatMessagesPage: listMessages,
      getPendingChatTask: getPending,
      listChatSessionWakeups: listWakeups,
      listTaskMessages,
    } as unknown as ApiClient);

    const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    await prefetchChatSession(qc, "s-1");

    expect(listMessages).toHaveBeenCalledTimes(1);
    expect(getPending).toHaveBeenCalledWith("s-1");
    expect(listWakeups).toHaveBeenCalledWith("s-1");
    // Only the newest two valid UUID assistant task timelines are warmed.
    expect(listTaskMessages.mock.calls.map(([id]) => id)).toEqual([t3, t2]);
    expect(qc.getQueryData(chatKeys.pendingTask("s-1"))).toEqual({});
    expect(qc.getQueryData(chatKeys.wakeups("s-1"))).toEqual([]);

    // Second call hits the Infinity-staleTime cache without refetching.
    await prefetchChatSession(qc, "s-1");
    expect(listMessages).toHaveBeenCalledTimes(1);
    expect(getPending).toHaveBeenCalledTimes(1);
    expect(listWakeups).toHaveBeenCalledTimes(1);
    expect(listTaskMessages).toHaveBeenCalledTimes(2);
  });

  it("is a no-op for an empty session id", async () => {
    const listMessages = vi.fn();
    setApiInstance({ listChatMessagesPage: listMessages } as unknown as ApiClient);
    const qc = new QueryClient();
    await prefetchChatSession(qc, "");
    expect(listMessages).not.toHaveBeenCalled();
  });
});
