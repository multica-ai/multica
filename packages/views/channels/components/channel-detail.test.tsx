import { describe, it, expect, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel } from "@multica/core/types";

const mockArchive = vi.hoisted(() => vi.fn());
const mockMarkRead = vi.hoisted(() => vi.fn());

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

vi.mock("@multica/core/channels", () => ({
  channelDetailOptions: () => ({
    queryKey: ["channels", "ws", "detail", "c1"],
    queryFn: () => Promise.resolve(undefined),
  }),
  channelKeys: {
    list: () => ["channels", "ws", "list"],
    detail: () => ["channels", "ws", "detail", "c1"],
  },
}));

vi.mock("@multica/core/inbox/queries", () => ({
  inboxKeys: { list: () => ["inbox", "ws", "list"] },
  inboxListOptions: () => ({
    queryKey: ["inbox", "ws", "list"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/inbox/mutations", () => ({
  useMarkInboxRead: () => ({ mutate: mockMarkRead }),
  useArchiveInbox: () => ({ mutate: mockArchive }),
}));

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      const last = options.queryKey?.[options.queryKey.length - 1];
      if (last === "list") return { data: [] };
      return { data: undefined };
    },
    useQueryClient: () => ({
      setQueryData: vi.fn(),
      invalidateQueries: vi.fn(),
    }),
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

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: React.ReactElement }) => render,
  TooltipContent: ({ children }: { children: React.ReactNode }) => (
    <span>{children}</span>
  ),
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
  created_at: new Date().toISOString(),
  updated_at: new Date().toISOString(),
};

describe("ChannelDetail thread header", () => {
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

  it("disables the Pin button until JEH-592 lands", () => {
    render(<ChannelDetail channelId="c1" initialChannel={baseChannel} />);
    const pin = screen.getByLabelText("Pin to sidebar");
    expect(pin).toBeDisabled();
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
    // No inbox row exists in this test, so archive is a no-op on the
    // mutation but the parent is still told to clear its selection.
    expect(onArchive).toHaveBeenCalledTimes(1);
  });
});
