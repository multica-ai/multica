// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { ChatSession } from "../types/chat";
import {
  earlierChatSessions,
  pickPrimaryChatSession,
  unreadChatMessagesByAgent,
} from "./primary-session";

function session(over: Partial<ChatSession> & { id: string }): ChatSession {
  return {
    workspace_id: "ws",
    agent_id: "a1",
    creator_id: "u1",
    title: over.id,
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("pickPrimaryChatSession", () => {
  it("picks the most recently active direct session of the agent", () => {
    const sessions = [
      session({ id: "old", updated_at: "2026-01-02T00:00:00Z" }),
      session({
        id: "recent",
        updated_at: "2026-01-01T00:00:00Z",
        last_message: { content: "hi", role: "user", created_at: "2026-01-05T00:00:00Z" },
      }),
      session({ id: "other-agent", agent_id: "a2", updated_at: "2026-02-01T00:00:00Z" }),
    ];
    expect(pickPrimaryChatSession(sessions, "a1")?.id).toBe("recent");
  });

  it("never picks archived or channel-bound sessions", () => {
    const sessions = [
      session({ id: "archived", status: "archived", updated_at: "2026-03-01T00:00:00Z" }),
      session({
        id: "slack",
        updated_at: "2026-03-01T00:00:00Z",
        channel_source: { channel_type: "slack", installation_id: "i", route_revision: 1 },
      }),
    ];
    expect(pickPrimaryChatSession(sessions, "a1")).toBeNull();
  });
});

describe("earlierChatSessions", () => {
  it("lists every other session of the agent, newest first", () => {
    const sessions = [
      session({ id: "p", updated_at: "2026-01-09T00:00:00Z" }),
      session({ id: "e1", updated_at: "2026-01-02T00:00:00Z" }),
      session({ id: "e2", status: "archived", updated_at: "2026-01-03T00:00:00Z" }),
      session({ id: "x", agent_id: "a2" }),
    ];
    expect(earlierChatSessions(sessions, "a1", "p").map((s) => s.id)).toEqual(["e2", "e1"]);
  });
});

describe("unreadChatMessagesByAgent", () => {
  it("sums unread per agent, skipping archived and the viewed session", () => {
    const sessions = [
      session({ id: "a", unread_count: 2 }),
      session({ id: "b", unread_count: 1 }),
      session({ id: "c", unread_count: 5, status: "archived" }),
      session({ id: "d", agent_id: "a2", unread_count: 3 }),
    ];
    const byAgent = unreadChatMessagesByAgent(sessions, "b");
    expect(byAgent.get("a1")).toBe(2);
    expect(byAgent.get("a2")).toBe(3);
  });
});
