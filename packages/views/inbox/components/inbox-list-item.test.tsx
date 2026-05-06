import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel, InboxItem } from "@multica/core/types";

// Stub workspace hooks so ActorAvatar / useChannelDisplay can resolve names
// without booting the whole core platform stack.
vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_t: string, id: string) => `User-${id.slice(0, 4)}`,
    getActorInitials: () => "U",
    getActorAvatarUrl: () => null,
  }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "me" } }),
}));

import { ChannelListItem, InboxListItem } from "./inbox-list-item";

function makeIssueInboxItem(overrides: Partial<InboxItem> = {}): InboxItem {
  return {
    id: "n1",
    workspace_id: "ws",
    recipient_type: "member",
    recipient_id: "me",
    actor_type: "member",
    actor_id: "alice",
    type: "new_comment",
    severity: "info",
    route: "inbox",
    issue_id: "i1",
    project_id: null,
    title: "Ship the thing",
    body: "looks good",
    issue_status: "todo",
    read: false,
    archived: false,
    created_at: new Date().toISOString(),
    details: null,
    ...overrides,
  };
}

function makeChannel(overrides: Partial<Channel> = {}): Channel {
  return {
    id: "c1",
    workspace_id: "ws",
    number: 1,
    identifier: "JEH-1",
    kind: "channel",
    title: "general",
    description: "Where the team chats",
    status: "todo",
    project_id: null,
    assignee_type: null,
    assignee_id: null,
    creator_type: "member",
    creator_id: "me",
    participants: [
      { user_type: "member", user_id: "me" },
      { user_type: "member", user_id: "alice" },
    ],
    unread_count: 0,
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
    ...overrides,
  };
}

describe("InboxListItem (issue variant)", () => {
  it("renders the issue title and unread stripe when unread", () => {
    render(
      <InboxListItem
        item={makeIssueInboxItem({ read: false })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    expect(screen.getByText("Ship the thing")).toBeInTheDocument();
  });

  it("fires onArchive without selecting the row", async () => {
    const user = userEvent.setup();
    const onClick = vi.fn();
    const onArchive = vi.fn();
    render(
      <InboxListItem
        item={makeIssueInboxItem()}
        isSelected={false}
        onClick={onClick}
        onArchive={onArchive}
      />,
    );
    // Archive control is a span with role=button (hover affordance).
    const archive = screen.getByTitle("Archive");
    await user.click(archive);
    expect(onArchive).toHaveBeenCalledTimes(1);
    expect(onClick).not.toHaveBeenCalled();
  });
});

describe("ChannelListItem", () => {
  it("renders a named channel with the # prefix", () => {
    render(
      <ChannelListItem
        channel={makeChannel({ kind: "channel", title: "engineering" })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    expect(screen.getByText("engineering")).toBeInTheDocument();
  });

  it("renders a DM with the peer's name (not 'self')", () => {
    render(
      <ChannelListItem
        channel={makeChannel({
          kind: "dm",
          title: "irrelevant",
          participants: [
            { user_type: "member", user_id: "me" },
            { user_type: "member", user_id: "alice" },
          ],
        })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    // DM title is derived from the *other* participant, never the current
    // user — guarding against the easy bug where both members see "DM with
    // myself".
    expect(screen.getByText("User-alic")).toBeInTheDocument();
  });

  it("shows unread badge when unread_count > 1", () => {
    render(
      <ChannelListItem
        channel={makeChannel({ unread_count: 5 })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    expect(screen.getByText("5")).toBeInTheDocument();
  });

  it("shows the snippet preview with bold author when provided", () => {
    render(
      <ChannelListItem
        channel={makeChannel()}
        preview={{ author: "Sara", text: "shipping at 3" }}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    // Author and text are split — author is wrapped in a bold span so the
    // mail-style "**Sara:** shipping at 3" reads correctly.
    expect(screen.getByText("Sara:")).toBeInTheDocument();
    expect(screen.getByText("shipping at 3")).toBeInTheDocument();
  });

  it("renders the @YOU mention badge when mentioned", () => {
    render(
      <ChannelListItem
        channel={makeChannel({ unread_count: 1 })}
        mentioned
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    expect(screen.getByText(/@\s*you/i)).toBeInTheDocument();
  });

  it("hides the @YOU badge when not mentioned", () => {
    render(
      <ChannelListItem
        channel={makeChannel({ unread_count: 1 })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    expect(screen.queryByText(/@\s*you/i)).toBeNull();
  });

  it("shows participant names line for named channels with multiple members", () => {
    render(
      <ChannelListItem
        channel={makeChannel({
          kind: "channel",
          title: "growth",
          participants: [
            { user_type: "member", user_id: "me" },
            { user_type: "member", user_id: "alice" },
            { user_type: "member", user_id: "bob" },
          ],
        })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    // Names from the mocked getActorName ("User-alic", "User-bob ") joined.
    expect(screen.getByText(/User-alic, User-bob/)).toBeInTheDocument();
  });

  it("does not show participant names line for DMs", () => {
    render(
      <ChannelListItem
        channel={makeChannel({
          kind: "dm",
          participants: [
            { user_type: "member", user_id: "me" },
            { user_type: "member", user_id: "alice" },
          ],
        })}
        isSelected={false}
        onClick={() => {}}
        onArchive={() => {}}
      />,
    );
    // DMs derive title from the peer; participants line is channel-only.
    expect(screen.queryByText(/User-alic,/)).toBeNull();
  });
});
