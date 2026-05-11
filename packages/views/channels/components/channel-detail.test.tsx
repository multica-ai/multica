import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel } from "@multica/core/types";

const mockArchive = vi.hoisted(() => vi.fn());
const mockArchiveChannel = vi.hoisted(() => vi.fn());
const mockMarkChannelRead = vi.hoisted(() => vi.fn());
const mockCreatePin = vi.hoisted(() => vi.fn());
const mockDeletePin = vi.hoisted(() => vi.fn());
const mockUpdateChannel = vi.hoisted(() => vi.fn());
const mockToggleParticipant = vi.hoisted(() => vi.fn());
const pinListData = vi.hoisted(() => ({ items: [] as Array<{ item_type: string; item_id: string }> }));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "me" } }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws",
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_t: string, id: string) => `User-${id}`,
    getActorInitials: () => "U",
    getActorAvatarUrl: () => null,
  }),
}));

const mockSetListenMode = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/channels", () => ({
  channelDetailOptions: () => ({
    queryKey: ["channels", "ws", "detail", "c1"],
    queryFn: () => Promise.resolve(undefined),
  }),
  channelAgentSettingsOptions: () => ({
    queryKey: ["channels", "ws", "agent-settings", "c1"],
    queryFn: () => Promise.resolve({ settings: [] }),
  }),
  useMarkChannelRead: () => ({ mutate: mockMarkChannelRead }),
  useUpdateChannel: () => ({ mutate: mockUpdateChannel }),
  useToggleChannelParticipant: () => ({ mutate: mockToggleParticipant }),
  useSetChannelAgentListenMode: () => ({
    mutate: mockSetListenMode,
    isPending: false,
  }),
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxListOptions: () => ({
    queryKey: ["inbox", "ws", "list"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/inbox/mutations", () => ({
  useArchiveInbox: () => ({ mutate: mockArchive }),
}));

vi.mock("@multica/cerebro-channels", async () => {
  const actual = await vi.importActual<typeof import("@multica/cerebro-channels")>(
    "@multica/cerebro-channels",
  );
  return {
    ...actual,
    useArchiveChannel: () => ({ mutate: mockArchiveChannel }),
  };
});

vi.mock("@multica/core/pins", () => ({
  pinListOptions: () => ({
    queryKey: ["pins", "ws", "me", "list"],
    queryFn: () => Promise.resolve(pinListData.items),
  }),
  useCreatePin: () => ({ mutate: mockCreatePin }),
  useDeletePin: () => ({ mutate: mockDeletePin }),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      const first = options.queryKey?.[0];
      const last = options.queryKey?.[options.queryKey.length - 1];
      if (first === "pins" && last === "list") return { data: pinListData.items };
      if (last === "list") return { data: [] };
      return { data: undefined };
    },
  };
});

vi.mock("../../issues/hooks/use-issue-timeline", () => ({
  useIssueTimeline: () => ({
    timeline: [],
    submitComment: vi.fn(),
    submitReply: vi.fn(),
    editComment: vi.fn(),
    deleteComment: vi.fn(),
    toggleReaction: vi.fn(),
  }),
}));

// CommentCard / CommentInput rely on tiptap; stub them to keep the test
// focused on the thread header + archive plumbing.
vi.mock("../../issues/components/comment-card", () => ({
  CommentCard: () => <div data-testid="comment-card" />,
}));

vi.mock("../../issues/components/comment-input", () => ({
  CommentInput: ({ issueId }: { issueId: string }) => (
    <div data-testid="comment-input" data-issue={issueId} />
  ),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("@multica/ui/components/ui/skeleton", () => ({
  Skeleton: ({ className }: { className?: string }) => (
    <div className={className} data-testid="skeleton" />
  ),
}));

vi.mock("@multica/ui/components/ui/avatar", () => ({
  AvatarGroup: ({ children }: { children: React.ReactNode }) => (
    <div data-testid="avatar-group">{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

// ParticipantsPanel renders a Sheet + AlertDialog tree we don't need in this
// test — stub it to a passive observer of the open prop.
vi.mock("./participants-panel", () => ({
  ParticipantsPanel: ({ open }: { open: boolean }) => (
    <div data-testid="participants-panel" data-open={open ? "1" : "0"} />
  ),
}));

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactElement }) => render,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
}));

// ChannelAgentInlineRow subscribes to WS events and calls api.getActiveTasksForIssue.
// Tests focus on the thread header + archive plumbing, so stub both out.
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: vi.fn(),
  useWSReconnect: vi.fn(),
}));
vi.mock("@multica/core/api", () => ({
  api: {
    getActiveTasksForIssue: vi.fn(() => Promise.resolve({ tasks: [] })),
    cancelTask: vi.fn(),
  },
}));

import { ChannelDetail } from "./channel-detail";

const baseChannel: Channel = {
  id: "c1",
  workspace_id: "ws",
  number: 1,
  identifier: "JEH-1",
  kind: "channel",
  title: "growth",
  description: "Ugentlige diskussioner om customer acquisition",
  status: "todo",
  project_id: null,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "me",
  participants: [
    { user_type: "member", user_id: "me" },
    { user_type: "member", user_id: "alice" },
    { user_type: "agent", user_id: "lando" },
  ],
  unread_count: 0,
  last_message: null,
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
};

describe("ChannelDetail thread header", () => {
  beforeEach(() => {
    mockMarkChannelRead.mockClear();
    mockArchive.mockClear();
    mockArchiveChannel.mockClear();
  });

  it("renders channel title, description and participant stack (excluding self)", () => {
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    expect(screen.getByText("growth")).toBeInTheDocument();
    expect(
      screen.getByText("Ugentlige diskussioner om customer acquisition"),
    ).toBeInTheDocument();
    // Self ("me") never appears in the stack — channels show *others*.
    expect(screen.queryByTestId("avatar-me")).not.toBeInTheDocument();
    expect(screen.getByTestId("avatar-alice")).toBeInTheDocument();
    expect(screen.getByTestId("avatar-lando")).toBeInTheDocument();
  });

  it("pins the channel when not yet pinned", async () => {
    pinListData.items = [];
    mockCreatePin.mockClear();
    mockDeletePin.mockClear();
    const user = userEvent.setup();
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    const pin = screen.getByLabelText("Pin to sidebar");
    expect(pin).not.toBeDisabled();
    expect(pin).toHaveAttribute("aria-pressed", "false");
    await user.click(pin);
    expect(mockCreatePin).toHaveBeenCalledWith({ item_type: "channel", item_id: "c1" });
    expect(mockDeletePin).not.toHaveBeenCalled();
  });

  it("unpins when already pinned", async () => {
    pinListData.items = [{ item_type: "channel", item_id: "c1" }];
    mockCreatePin.mockClear();
    mockDeletePin.mockClear();
    const user = userEvent.setup();
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    const pin = screen.getByLabelText("Unpin from sidebar");
    expect(pin).toHaveAttribute("aria-pressed", "true");
    await user.click(pin);
    expect(mockDeletePin).toHaveBeenCalledWith({ itemType: "channel", itemId: "c1" });
    expect(mockCreatePin).not.toHaveBeenCalled();
  });

  it("uses item_type 'dm' for direct-message channels", async () => {
    pinListData.items = [];
    mockCreatePin.mockClear();
    const user = userEvent.setup();
    const dm: Channel = { ...baseChannel, kind: "dm", title: "alice" };
    render(<ChannelDetail channelId="c1" initialChannel={dm} />);
    await user.click(screen.getByLabelText("Pin to sidebar"));
    expect(mockCreatePin).toHaveBeenCalledWith({ item_type: "dm", item_id: "c1" });
  });

  it("archives the conversation and notifies the parent", async () => {
    const user = userEvent.setup();
    const onArchive = vi.fn();
    render(
      <ChannelDetail
        channelId="c1"
        initialChannel={baseChannel}
        onArchive={onArchive}
      />,
    );
    await user.click(screen.getByLabelText("Archive conversation"));
    // Channel archive always fires, regardless of whether an inbox row
    // exists. Parent is also told to clear its selection.
    expect(mockArchiveChannel).toHaveBeenCalledWith("c1");
    expect(onArchive).toHaveBeenCalledTimes(1);
  });

  it("calls mark-channel-read once on mount when unread_count > 0", () => {
    const unread: Channel = { ...baseChannel, unread_count: 3 };
    const { rerender } = render(
      <ChannelDetail channelId="c1" initialChannel={unread} />,
    );
    expect(mockMarkChannelRead).toHaveBeenCalledTimes(1);
    expect(mockMarkChannelRead).toHaveBeenCalledWith("c1");

    // Re-render the same channel — must not fire again (idempotent per mount).
    rerender(<ChannelDetail channelId="c1" initialChannel={unread} />);
    expect(mockMarkChannelRead).toHaveBeenCalledTimes(1);
  });

  it("does not call mark-channel-read when unread_count is 0", () => {
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    expect(mockMarkChannelRead).not.toHaveBeenCalled();
  });

  it("opens the participants panel when the participant stack is clicked", async () => {
    const user = userEvent.setup();
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    expect(screen.getByTestId("participants-panel")).toHaveAttribute(
      "data-open",
      "0",
    );
    await user.click(screen.getByLabelText("Open participants"));
    expect(screen.getByTestId("participants-panel")).toHaveAttribute(
      "data-open",
      "1",
    );
  });

  it("renames a channel when the title is clicked, edited and submitted", async () => {
    mockUpdateChannel.mockClear();
    const user = userEvent.setup();
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    await user.click(screen.getByLabelText("Rename channel"));
    const input = screen.getByLabelText("Channel name") as HTMLInputElement;
    await user.clear(input);
    await user.type(input, "new-growth{Enter}");
    expect(mockUpdateChannel).toHaveBeenCalledWith(
      { id: "c1", title: "new-growth" },
      expect.any(Object),
    );
  });

  it("does not call rename when the title is unchanged or empty", async () => {
    mockUpdateChannel.mockClear();
    const user = userEvent.setup();
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    await user.click(screen.getByLabelText("Rename channel"));
    const input = screen.getByLabelText("Channel name") as HTMLInputElement;
    // Submit unchanged value — no mutation.
    await user.type(input, "{Enter}");
    expect(mockUpdateChannel).not.toHaveBeenCalled();
  });

  it("does not show a renameable title for DMs", () => {
    const dm: Channel = { ...baseChannel, kind: "dm", title: "alice" };
    render(<ChannelDetail channelId="c1" initialChannel={dm} />);
    expect(screen.queryByLabelText("Rename channel")).not.toBeInTheDocument();
  });
});
