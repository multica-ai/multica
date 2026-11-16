import { describe, expect, it } from "vitest";
import { countUnreadChatMessages } from "./unread";
import type { ChatSession } from "../types/chat";

function session(id: string, unread_count?: number): ChatSession {
  return {
    id,
    workspace_id: "ws-1",
    agent_id: "agent-1",
    creator_id: "user-1",
    title: id,
    status: "active",
    has_unread: (unread_count ?? 0) > 0,
    unread_count,
    created_at: "2026-07-10T00:00:00Z",
    updated_at: "2026-07-10T00:00:00Z",
  };
}

describe("countUnreadChatMessages", () => {
  it("sums unread messages across sessions (not session count)", () => {
    expect(
      countUnreadChatMessages([session("a", 2), session("b", 3), session("c", 0)]),
    ).toBe(5);
  });

  it("treats a missing unread_count (older server) as 0", () => {
    expect(countUnreadChatMessages([session("a"), session("b", 4)])).toBe(4);
  });

  it("returns 0 for an empty or unloaded list", () => {
    expect(countUnreadChatMessages([])).toBe(0);
    expect(countUnreadChatMessages(undefined)).toBe(0);
  });

  it("excludes the session being viewed from the sum", () => {
    const sessions = [session("a", 2), session("b", 3)];
    expect(countUnreadChatMessages(sessions, "a")).toBe(3);
    expect(countUnreadChatMessages(sessions, "b")).toBe(2);
  });

  it("ignores an exclude id that is not in the list", () => {
    expect(countUnreadChatMessages([session("a", 2)], "ghost")).toBe(2);
    expect(countUnreadChatMessages([session("a", 2)], null)).toBe(2);
  });
});
