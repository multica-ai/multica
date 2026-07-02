import { describe, expect, it } from "vitest";
import type { ChatSession, InboxItem } from "@multica/core/types";
import {
  conversationEntry,
  getInboxItemRenderer,
  hasInboxItemRenderer,
  notificationEntry,
} from "./index";

function item(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "inbox-1",
    workspace_id: "workspace-1",
    recipient_type: "member",
    recipient_id: "member-1",
    actor_type: "agent",
    actor_id: "agent-1",
    type: "new_comment",
    severity: "info",
    issue_id: "issue-1",
    title: "Issue title",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    created_at: "2026-04-29T12:00:00Z",
    details: null,
    ...overrides,
  };
}

function session(overrides: Partial<ChatSession> = {}): ChatSession {
  return {
    id: "session-1",
    workspace_id: "workspace-1",
    agent_id: "agent-1",
    creator_id: "member-1",
    title: "Fix login redirect",
    status: "active",
    has_unread: true,
    created_at: "2026-04-29T11:00:00Z",
    updated_at: "2026-04-29T13:00:00Z",
    ...overrides,
  };
}

describe("inbox feed entries", () => {
  it("projects a notification onto an issue_notification entry", () => {
    const entry = notificationEntry(item({ id: "n1", read: false }));
    expect(entry.kind).toBe("issue_notification");
    expect(entry.id).toBe("n1");
    expect(entry.unread).toBe(true);
    expect(entry.sortAt).toBe("2026-04-29T12:00:00Z");
  });

  it("projects a chat session onto a conversation entry sorted by updated_at", () => {
    const entry = conversationEntry(session({ id: "s1", has_unread: false }));
    expect(entry.kind).toBe("conversation");
    expect(entry.id).toBe("s1");
    expect(entry.unread).toBe(false);
    expect(entry.sortAt).toBe("2026-04-29T13:00:00Z");
  });
});

describe("inbox item-type registry", () => {
  it("registers renderers for both shipping kinds", () => {
    expect(hasInboxItemRenderer("issue_notification")).toBe(true);
    expect(hasInboxItemRenderer("conversation")).toBe(true);

    const notif = getInboxItemRenderer("issue_notification");
    expect(typeof notif.Row).toBe("function");
    expect(typeof notif.Detail).toBe("function");

    const convo = getInboxItemRenderer("conversation");
    expect(typeof convo.Row).toBe("function");
    // Conversations open inline in the detail pane (like an issue).
    expect(typeof convo.Detail).toBe("function");
  });
});
