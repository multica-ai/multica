import { describe, it, expect } from "vitest";
import type { Channel, ChatSession } from "@multica/core/types";
import { countUnreadConversations } from "./use-chat-unread-count";
import { DEFAULT_PLACEMENT, setPlace, type ChatPlacement } from "./chat-placement";

const ALL_IN_CHAT: ChatPlacement = {
  channel: { chat: true, inbox: true },
  dm: { chat: true, inbox: true },
  agent_chat: { chat: true, inbox: true },
};

const NONE_IN_CHAT: ChatPlacement = {
  channel: { chat: false, inbox: true },
  dm: { chat: false, inbox: true },
  agent_chat: { chat: false, inbox: true },
};

function channel(over: Partial<Channel>): Channel {
  return {
    id: "c1",
    workspace_id: "w",
    number: 1,
    identifier: "C-1",
    kind: "channel",
    title: "general",
    description: null,
    status: "todo",
    project_id: null,
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "u",
    participants: [],
    unread_count: 0,
    last_message: null,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

function session(over: Partial<ChatSession>): ChatSession {
  return {
    id: "s1",
    workspace_id: "w",
    agent_id: "a",
    creator_id: "u",
    title: "chat",
    status: "active",
    has_unread: false,
    created_at: "2026-01-01T00:00:00Z",
    updated_at: "2026-01-01T00:00:00Z",
    ...over,
  };
}

describe("countUnreadConversations", () => {
  it("counts nothing while nothing is placed in Chat", () => {
    const count = countUnreadConversations(
      [channel({ unread_count: 3 })],
      [session({ has_unread: true })],
      NONE_IN_CHAT,
    );
    expect(count).toBe(0);
  });

  it("counts channels/DMs but not agents under the default", () => {
    // FIR-4350 — the default places channels and DMs in Chat, but agents are
    // inbox-only, so an unread agent chat is NOT counted in the Chat badge.
    const count = countUnreadConversations(
      [channel({ unread_count: 3 })],
      [session({ has_unread: true })],
      DEFAULT_PLACEMENT,
    );
    expect(count).toBe(1);
  });

  it("counts an unread channel once, however many messages are unread", () => {
    expect(
      countUnreadConversations([channel({ unread_count: 7 })], [], ALL_IN_CHAT),
    ).toBe(1);
  });

  it("counts a channel whose only signal is has_unread_activity", () => {
    expect(
      countUnreadConversations(
        [channel({ unread_count: 0, has_unread_activity: true })],
        [],
        ALL_IN_CHAT,
      ),
    ).toBe(1);
  });

  it("ignores a read channel", () => {
    expect(countUnreadConversations([channel({})], [], ALL_IN_CHAT)).toBe(0);
  });

  it("counts channels and agent chats together", () => {
    const count = countUnreadConversations(
      [channel({ unread_count: 1 }), channel({ id: "c2", kind: "dm", unread_count: 2 })],
      [session({ has_unread: true })],
      ALL_IN_CHAT,
    );
    expect(count).toBe(3);
  });

  it("excludes a type that is not placed in Chat", () => {
    const noDmInChat = setPlace(ALL_IN_CHAT, "dm", "chat", false);
    const count = countUnreadConversations(
      [channel({ unread_count: 1 }), channel({ id: "c2", kind: "dm", unread_count: 2 })],
      [],
      noDmInChat,
    );
    expect(count).toBe(1);
  });

  it("places a group channel with DMs", () => {
    const noDmInChat = setPlace(ALL_IN_CHAT, "dm", "chat", false);
    expect(
      countUnreadConversations(
        [channel({ kind: "group", unread_count: 1 })],
        [],
        noDmInChat,
      ),
    ).toBe(0);
  });

  it("ignores archived agent chat sessions", () => {
    expect(
      countUnreadConversations(
        [],
        [session({ status: "archived", has_unread: true })],
        ALL_IN_CHAT,
      ),
    ).toBe(0);
  });
});
