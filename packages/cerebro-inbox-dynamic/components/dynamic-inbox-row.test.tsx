// @vitest-environment jsdom
// TECH-3709 — the open (selected) row reads as "read" while you're active on
// it: the bold weight + brand-blue unread stripe drop on selection, but the row
// keeps its box/group (placement is frozen by the selection-time snapshot in
// dynamic-inbox.tsx). These tests assert the row hands a read-marked entry to
// the shared row components when selected, and that the local chat row drops its
// own unread chrome.
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, render, screen } from "@testing-library/react";
import type { ChatSession, Channel, InboxItem } from "@multica/core/types";
import type { DynInboxEntry } from "../section-filter";

afterEach(() => cleanup());

// Stub the shared upstream row components so we can read back exactly what
// DynamicInboxRow passes them (the real read→bold mapping is covered by
// inbox-list-item.test.tsx). useTimeAgo / AgentRunPip are used by the local
// chat row, so they get lightweight stand-ins.
vi.mock("@multica/views/inbox/components/inbox-list-item", () => ({
  InboxListItem: ({ item, isSelected }: { item: InboxItem; isSelected: boolean }) => (
    <div
      data-testid="notif-row"
      data-read={String(!!item.read)}
      data-selected={String(isSelected)}
    />
  ),
  ChannelListItem: ({
    channel,
    mentioned,
    isSelected,
  }: {
    channel: Channel;
    mentioned?: boolean;
    isSelected: boolean;
  }) => (
    <div
      data-testid="channel-row"
      data-unread-count={String(channel.unread_count)}
      data-mentioned={String(!!mentioned)}
      data-selected={String(isSelected)}
    />
  ),
  useTimeAgo: () => () => "just now",
  AgentRunPip: () => null,
}));

vi.mock("@multica/views/common/actor-avatar", () => ({
  ActorAvatar: () => <span data-testid="avatar" />,
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({ getActorName: (_t: string, id: string) => `Agent-${id}` }),
}));

vi.mock("@multica/cerebro-chat/views", () => ({
  CerebroChatSessionRowActions: () => null,
}));

import { DynamicInboxRow } from "./dynamic-inbox-row";

function notifEntry(read: boolean): DynInboxEntry {
  return {
    kind: "notif",
    id: "n1",
    time: 0,
    item: { id: "n1", read, type: "new_comment", issue_id: "i1", title: "Hi" } as InboxItem,
  };
}

function channelEntry(unread: number): DynInboxEntry {
  return {
    kind: "channel",
    id: "c1",
    time: 0,
    channel: { id: "c1", unread_count: unread, kind: "channel", title: "general" } as Channel,
  };
}

function chatEntry(hasUnread: boolean): DynInboxEntry {
  return {
    kind: "chat",
    id: "s1",
    time: 0,
    session: {
      id: "s1",
      agent_id: "a1",
      title: "Chat",
      has_unread: hasUnread,
      updated_at: new Date().toISOString(),
    } as ChatSession,
  };
}

const noop = () => {};

describe("DynamicInboxRow read-on-select (TECH-3709)", () => {
  it("keeps an unread notif row unread when not selected", () => {
    render(
      <DynamicInboxRow entry={notifEntry(false)} isSelected={false} onSelect={noop} onArchive={noop} />,
    );
    expect(screen.getByTestId("notif-row").dataset.read).toBe("false");
  });

  it("marks the open notif row read once selected (without flipping the source)", () => {
    const entry = notifEntry(false);
    render(<DynamicInboxRow entry={entry} isSelected onSelect={noop} onArchive={noop} />);
    expect(screen.getByTestId("notif-row").dataset.read).toBe("true");
    // The underlying entry/item is untouched — only the rendered copy is read,
    // so placement (which still uses the snapshot) is unaffected.
    expect(entry.kind === "notif" && entry.item.read).toBe(false);
  });

  it("zeroes the unread count + mention on the open channel row", () => {
    render(<DynamicInboxRow entry={channelEntry(3)} isSelected mentioned onSelect={noop} onArchive={noop} />);
    const row = screen.getByTestId("channel-row");
    expect(row.dataset.unreadCount).toBe("0");
    expect(row.dataset.mentioned).toBe("false");
  });

  it("leaves an unselected channel row's unread count intact", () => {
    render(
      <DynamicInboxRow entry={channelEntry(3)} isSelected={false} onSelect={noop} onArchive={noop} />,
    );
    expect(screen.getByTestId("channel-row").dataset.unreadCount).toBe("3");
  });

  it("drops the brand unread stripe on the open chat row", () => {
    const { container, rerender } = render(
      <DynamicInboxRow entry={chatEntry(true)} isSelected={false} onSelect={noop} onArchive={noop} />,
    );
    expect(container.querySelector(".bg-brand")).not.toBeNull();
    rerender(<DynamicInboxRow entry={chatEntry(true)} isSelected onSelect={noop} onArchive={noop} />);
    expect(container.querySelector(".bg-brand")).toBeNull();
  });
});
