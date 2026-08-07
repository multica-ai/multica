import { describe, it, expect } from "vitest";
import type { Channel, InboxItem } from "@multica/core/types";
import { buildUnreadChannelThreads } from "./channel-thread-entries";
import { channelChatUnreadBadge, channelHasChatUnread } from "./channel-chat-unread";

function channel(over: Partial<Channel> = {}): Channel {
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

// Full InboxItem shape — partial `as InboxItem[]` fails tsc (TS2352).
function inboxItem(over: Partial<InboxItem> = {}): InboxItem {
  return {
    id: over.id ?? "item",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "member",
    actor_id: "u1",
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "c1",
    project_id: null,
    title: "t",
    body: null,
    issue_status: null,
    read: false,
    archived: false,
    muted_until: null,
    created_at: "2026-01-02T00:00:00Z",
    details: null,
    ...over,
  } as InboxItem;
}

describe("channelHasChatUnread / channelChatUnreadBadge (FIR-4649)", () => {
  it("treats unread_count as unread", () => {
    expect(channelHasChatUnread(channel({ unread_count: 3 }))).toBe(true);
    expect(channelChatUnreadBadge(channel({ unread_count: 3 }))).toBe(3);
  });

  it("treats has_unread_activity alone as unread (smart-unread)", () => {
    expect(
      channelHasChatUnread(channel({ unread_count: 0, has_unread_activity: true })),
    ).toBe(true);
    expect(
      channelChatUnreadBadge(channel({ unread_count: 0, has_unread_activity: true })),
    ).toBe(1);
  });

  it("is read when neither signal is set", () => {
    expect(channelHasChatUnread(channel({}))).toBe(false);
    expect(channelChatUnreadBadge(channel({}))).toBe(0);
  });
});

describe("buildUnreadChannelThreads (FIR-4649 shared with Chat rail)", () => {
  it("splits an unread channel thread reply into its own row", () => {
    const map = new Map([["c1", channel()]]);
    const items = [
      inboxItem({
        id: "i1",
        issue_id: "c1",
        read: false,
        archived: false,
        created_at: "2026-01-02T00:00:00Z",
        details: {
          thread_root_id: "root1",
          thread_root_preview: "hello",
          comment_id: "cmt1",
        },
      }),
    ];
    const out = buildUnreadChannelThreads(items, map);
    expect(out).toHaveLength(1);
    expect(out[0]).toMatchObject({
      threadRootId: "root1",
      channelId: "c1",
      unreadCount: 1,
    });
  });

  it("skips a fully-read thread", () => {
    const map = new Map([["c1", channel()]]);
    const items = [
      inboxItem({
        id: "i1",
        issue_id: "c1",
        read: true,
        archived: false,
        details: { thread_root_id: "root1", comment_id: "cmt1" },
      }),
    ];
    expect(buildUnreadChannelThreads(items, map)).toEqual([]);
  });
});
