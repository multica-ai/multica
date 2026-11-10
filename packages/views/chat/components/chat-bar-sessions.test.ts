import { describe, expect, it } from "vitest";
import type { ChatSession } from "@multica/core/types";
import { selectChatBarSessions } from "./chat-bar-sessions";

function session(id: string, minute: number, overrides: Partial<ChatSession> = {}): ChatSession {
  const at = new Date(Date.UTC(2026, 10, 11, 8, minute)).toISOString();
  return {
    id,
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: id,
    status: "active",
    has_unread: false,
    created_at: at,
    updated_at: at,
    ...overrides,
  };
}

const ids = (items: { session: ChatSession }[]) => items.map((item) => item.session.id);

describe("selectChatBarSessions", () => {
  it("keeps only chats that are running, unread or open", () => {
    const { visible, overflow } = selectChatBarSessions({
      sessions: [
        session("idle", 1),
        session("running", 2),
        session("unread", 3, { has_unread: true }),
        session("open", 4),
      ],
      runningSessionIds: new Set(["running"]),
      openSessionId: "open",
      maxVisible: 4,
    });

    expect(ids(visible)).toEqual(["open", "unread", "running"]);
    expect(visible.map((item) => [item.running, item.unread, item.open])).toEqual([
      [false, false, true],
      [false, true, false],
      [true, false, false],
    ]);
    expect(overflow).toEqual([]);
  });

  it("leaves archived chats out even when they are unread or running", () => {
    const { visible } = selectChatBarSessions({
      sessions: [
        session("archived-unread", 1, { status: "archived", has_unread: true }),
        session("archived-running", 2, { status: "archived" }),
      ],
      runningSessionIds: new Set(["archived-running"]),
      openSessionId: null,
      maxVisible: 4,
    });

    expect(visible).toEqual([]);
  });

  it("orders by creation, so a newer message does not reorder the chips", () => {
    const { visible } = selectChatBarSessions({
      sessions: [
        session("older", 1, {
          has_unread: true,
          updated_at: new Date(Date.UTC(2026, 10, 11, 9)).toISOString(),
        }),
        session("newer", 2, { has_unread: true }),
      ],
      runningSessionIds: new Set(),
      openSessionId: null,
      maxVisible: 4,
    });

    expect(ids(visible)).toEqual(["newer", "older"]);
  });

  it("puts what does not fit into the overflow", () => {
    const { visible, overflow } = selectChatBarSessions({
      sessions: [1, 2, 3, 4].map((n) => session(`s${n}`, n, { has_unread: true })),
      runningSessionIds: new Set(),
      openSessionId: null,
      maxVisible: 2,
    });

    expect(ids(visible)).toEqual(["s4", "s3"]);
    expect(ids(overflow)).toEqual(["s2", "s1"]);
  });

  it("brings the open chat out of the overflow, because the window opens above its chip", () => {
    const { visible, overflow } = selectChatBarSessions({
      sessions: [1, 2, 3, 4].map((n) => session(`s${n}`, n, { has_unread: true })),
      runningSessionIds: new Set(),
      openSessionId: "s1",
      maxVisible: 2,
    });

    expect(ids(visible)).toEqual(["s1", "s4"]);
    expect(ids(overflow)).toEqual(["s3", "s2"]);
  });

  it("always has room for one chip", () => {
    const { visible } = selectChatBarSessions({
      sessions: [session("only", 1, { has_unread: true })],
      runningSessionIds: new Set(),
      openSessionId: null,
      maxVisible: 0,
    });

    expect(ids(visible)).toEqual(["only"]);
  });
});
