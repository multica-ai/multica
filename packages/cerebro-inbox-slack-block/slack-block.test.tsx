import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel, MemberWithUser } from "@multica/core/types";

const mockCreateChannel = vi.hoisted(() => vi.fn());

// Mutable presence/online state the mocked hook reads. Tests seed it before
// render. We keep it module-level (vi.hoisted) so the vi.mock factory can close
// over it without a temporal-dead-zone error.
const presenceState = vi.hoisted(() => ({
  online: new Set<string>(),
}));

// Captures the live `cerebro:typing` WS handler so a test can fire a typing
// event and assert "skriver…" appears. `useWSEvent(event, handler)` registers
// each call here keyed by event string.
const wsHandlers = vi.hoisted(
  () => new Map<string, (payload: unknown) => void>(),
);

const members: MemberWithUser[] = [
  {
    id: "m1",
    workspace_id: "ws",
    user_id: "me",
    role: "member",
    created_at: "",
    name: "Me",
    email: "me@example.com",
    avatar_url: null,
    budget_enforcement_enabled: false,
  },
  {
    id: "m2",
    workspace_id: "ws",
    user_id: "alice",
    role: "member",
    created_at: "",
    name: "Alice",
    email: "alice@example.com",
    avatar_url: null,
    budget_enforcement_enabled: false,
  },
  {
    id: "m3",
    workspace_id: "ws",
    user_id: "bob",
    role: "member",
    created_at: "",
    name: "Bob",
    email: "bob@example.com",
    avatar_url: null,
    budget_enforcement_enabled: false,
  },
];

const baseChannel = (over: Partial<Channel>): Channel => ({
  id: "c",
  workspace_id: "ws",
  number: 1,
  identifier: "JEH-1",
  kind: "channel",
  title: "",
  description: null,
  status: "todo",
  project_id: null,
  assignee_type: null,
  assignee_id: null,
  creator_type: "member",
  creator_id: "me",
  participants: [],
  unread_count: 0,
  last_message: null,
  created_at: "",
  updated_at: "",
  ...over,
});

// DM between me and Alice, currently selected and unread.
const aliceDm = baseChannel({
  id: "dm-alice",
  kind: "dm",
  title: "",
  unread_count: 2,
  participants: [
    { user_type: "member", user_id: "me" },
    { user_type: "member", user_id: "alice" },
  ],
});

const generalChannel = baseChannel({
  id: "chan-general",
  kind: "channel",
  title: "general",
  unread_count: 0,
});

const channels: Channel[] = [aliceDm, generalChannel];

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      const key = (options.queryKey?.[2] ?? options.queryKey?.[1]) as string;
      if (key === "members") return { data: members };
      if (key === "channels") return { data: channels };
      return { data: [] };
    },
  };
});

vi.mock("@multica/core/channels", () => ({
  channelListOptions: () => ({ queryKey: ["channels", "ws", "channels"] }),
  useCreateChannel: () => ({ mutateAsync: mockCreateChannel }),
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspaces", "ws", "members"] }),
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector: (s: { user: { id: string } }) => unknown) =>
      selector({ user: { id: "me" } }),
    { getState: () => ({ user: { id: "me" } }) },
  ),
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: () => true,
}));

// useWSEvent records each handler so tests can fire WS payloads at will.
vi.mock("@multica/core/realtime", () => ({
  useWSEvent: (event: string, handler: (payload: unknown) => void) => {
    wsHandlers.set(event, handler);
  },
}));

// Presence/typing REST calls — return the seeded online set, no network.
vi.mock("./api/presence-api", () => ({
  fetchPresenceSnapshot: async () => ({
    online_user_ids: Array.from(presenceState.online),
  }),
  pingPresence: async () => ({
    online_user_ids: Array.from(presenceState.online),
  }),
  postTyping: vi.fn(async () => {}),
}));

vi.mock("@multica/views/common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

import { SlackBlock } from "./components/slack-block";

const onlineDot = (userId: string) =>
  screen.getByTestId(`presence-dot-${userId}`);

describe("SlackBlock", () => {
  beforeEach(() => {
    mockCreateChannel.mockReset();
    mockCreateChannel.mockResolvedValue(aliceDm);
    presenceState.online = new Set();
    wsHandlers.clear();
  });

  it("renders a green online dot for a member in the online set, gray otherwise", async () => {
    presenceState.online = new Set(["alice"]);
    await act(async () => {
      render(
        <SlackBlock wsId="ws" selectedChannelId={null} onOpenChannel={() => {}} />,
      );
    });

    // Alice online -> bg-success; Bob offline -> bg-muted-foreground.
    expect(onlineDot("alice").className).toContain("bg-success");
    expect(onlineDot("alice")).toHaveAttribute("data-online", "true");
    expect(onlineDot("bob").className).toContain("bg-muted-foreground");
    expect(onlineDot("bob")).toHaveAttribute("data-online", "false");
  });

  it("renders channels with a # glyph", async () => {
    await act(async () => {
      render(
        <SlackBlock wsId="ws" selectedChannelId={null} onOpenChannel={() => {}} />,
      );
    });
    const row = screen.getByText("general").closest("button");
    expect(row).not.toBeNull();
    // lucide Hash renders an <svg> inside the channel row.
    expect(row?.querySelector("svg")).not.toBeNull();
  });

  it("clicking a channel row calls onOpenChannel with that channel", async () => {
    const onOpenChannel = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      render(
        <SlackBlock
          wsId="ws"
          selectedChannelId={null}
          onOpenChannel={onOpenChannel}
        />,
      );
    });

    await user.click(screen.getByText("general"));

    expect(onOpenChannel).toHaveBeenCalledWith(
      expect.objectContaining({ id: "chan-general", kind: "channel" }),
    );
  });

  it("shows 'skriver…' when a typing event arrives for a member's selected DM", async () => {
    await act(async () => {
      render(
        <SlackBlock
          wsId="ws"
          selectedChannelId="dm-alice"
          onOpenChannel={() => {}}
        />,
      );
    });

    expect(screen.queryByText("skriver…")).not.toBeInTheDocument();

    // Fire a typing event for Alice in her selected DM.
    await act(async () => {
      wsHandlers.get("cerebro:typing")?.({
        channel_id: "dm-alice",
        user_id: "alice",
      });
    });

    expect(screen.getByText("skriver…")).toBeInTheDocument();
  });
});
