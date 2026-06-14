import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, act, cleanup, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Channel, MemberWithUser } from "@multica/core/types";

const mockCreateChannel = vi.hoisted(() => vi.fn());

// Mutable presence/online state the mocked hook reads. Tests seed it before
// render. We keep it module-level (vi.hoisted) so the vi.mock factory can close
// over it without a temporal-dead-zone error.
const presenceState = vi.hoisted(() => ({
  online: new Set<string>(),
}));

// Mutable favorites set + spy for the starring tests.
const favState = vi.hoisted(() => ({
  keys: [] as string[],
  toggle: vi.fn((k: string) => {
    favState.keys = favState.keys.includes(k)
      ? favState.keys.filter((x) => x !== k)
      : [...favState.keys, k];
  }),
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

// TECH-3494 — agents for the opt-in "Vis agenter" list. One archived agent
// proves archived agents are filtered out.
const agents = [
  { id: "agent-sara", name: "Sara - CTO", archived_at: null },
  { id: "agent-old", name: "Old Bot", archived_at: "2026-01-01" },
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
      if (key === "agents") return { data: agents };
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
  agentListOptions: () => ({ queryKey: ["workspaces", "ws", "agents"] }),
}));

vi.mock("@multica/core/chat/queries", () => ({
  chatSessionsOptions: () => ({ queryKey: ["chat-sessions", "ws"] }),
}));

// canAssignAgent gate — every agent is assignable in these tests; archived
// filtering is exercised separately via the archived_at field.
vi.mock("@multica/views/issues/components", () => ({
  canAssignAgent: () => true,
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

// Favorites store: selector over the mutable favState. Starred tests seed
// favState.keys before render; the toggle spy records star clicks.
vi.mock("@multica/cerebro-channels", () => ({
  useChannelFavoritesStore: (
    selector: (s: { favorites: string[]; toggle: (k: string) => void }) => unknown,
  ) => selector({ favorites: favState.keys, toggle: favState.toggle }),
  actorKey: (type: string, id: string) => `${type}:${id}`,
  channelKey: (id: string) => `channel:${id}`,
}));

// Dropdown renders its content inline so the "Vis personer" options are in the
// DOM without opening a portal-backed menu in jsdom.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuTrigger: ({ render }: { render: React.ReactElement }) => render,
  DropdownMenuContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuItem: ({
    children,
    onClick,
  }: {
    children: React.ReactNode;
    onClick?: () => void;
  }) => (
    <button type="button" onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuLabel: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuGroup: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  DropdownMenuSeparator: () => <hr />,
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

// Fills the required section-chrome props so each test only overrides what it
// cares about.
function renderBlock(
  overrides: Partial<React.ComponentProps<typeof SlackBlock>> = {},
) {
  const props: React.ComponentProps<typeof SlackBlock> = {
    wsId: "ws",
    selectedChannelId: null,
    onOpenChannel: () => {},
    onSetLimit: () => {},
    onSetSort: () => {},
    onSetGroupBy: () => {},
    onSetShowAgents: () => {},
    onOpenAgentChat: () => {},
    onRemove: () => {},
    ...overrides,
  };
  return render(<SlackBlock {...props} />);
}

describe("SlackBlock", () => {
  beforeEach(() => {
    mockCreateChannel.mockReset();
    mockCreateChannel.mockResolvedValue(aliceDm);
    presenceState.online = new Set();
    favState.keys = [];
    favState.toggle.mockClear();
    wsHandlers.clear();
  });

  it("renders a green online dot for a member in the online set, gray otherwise", async () => {
    presenceState.online = new Set(["alice"]);
    await act(async () => {
      renderBlock();
    });

    expect(onlineDot("alice").className).toContain("bg-success");
    expect(onlineDot("alice")).toHaveAttribute("data-online", "true");
    expect(onlineDot("bob").className).toContain("bg-muted-foreground");
    expect(onlineDot("bob")).toHaveAttribute("data-online", "false");
  });

  it("renders channels with a # glyph", async () => {
    await act(async () => {
      renderBlock();
    });
    const row = screen.getByText("general").closest("button");
    expect(row).not.toBeNull();
    expect(row?.querySelector("svg")).not.toBeNull();
  });

  it("clicking a channel row calls onOpenChannel with that channel", async () => {
    const onOpenChannel = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onOpenChannel });
    });

    await user.click(screen.getByText("general"));

    expect(onOpenChannel).toHaveBeenCalledWith(
      expect.objectContaining({ id: "chan-general", kind: "channel" }),
    );
  });

  it("shows 'typing…' when a typing event arrives for a member's selected DM", async () => {
    await act(async () => {
      renderBlock({ selectedChannelId: "dm-alice" });
    });

    expect(screen.queryByText("typing…")).not.toBeInTheDocument();

    await act(async () => {
      wsHandlers.get("cerebro:typing")?.({
        channel_id: "dm-alice",
        user_id: "alice",
      });
    });

    expect(screen.getByText("typing…")).toBeInTheDocument();
  });

  // Reorder is drag-and-drop now (TECH-3494 removed the up/down arrows), so
  // only the remove control remains in the header.
  it("removes the block via the control box", async () => {
    const onRemove = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onRemove });
    });

    expect(screen.queryByTitle("Flyt op")).not.toBeInTheDocument();
    await user.click(screen.getByTitle("Remove block"));
    expect(onRemove).toHaveBeenCalled();
  });

  // TECH-3494 #2 — the whole interface is English.
  it("uses English labels in the settings menu", async () => {
    await act(async () => {
      renderBlock();
    });
    expect(screen.getByText("Sort by")).toBeInTheDocument();
    expect(screen.getByText("Group by")).toBeInTheDocument();
    // Defaults carry a trailing "✓", so match loosely.
    expect(screen.getByText(/Recent conversation/)).toBeInTheDocument();
    expect(screen.getByText("Name")).toBeInTheDocument();
    expect(screen.getByText(/By type/)).toBeInTheDocument();
    expect(screen.getByText("Flat list")).toBeInTheDocument();
    // No Danish remnants.
    expect(screen.queryByText("Ulæste øverst")).not.toBeInTheDocument();
    expect(screen.queryByText("Sortér efter")).not.toBeInTheDocument();
  });

  // TECH-3494 #5 — opt-in agents are part of the unified list.
  it("hides agents by default and shows assignable (non-archived) agents when enabled", async () => {
    await act(async () => {
      renderBlock({ showAgents: false });
    });
    expect(screen.queryByText("Sara - CTO")).not.toBeInTheDocument();

    cleanup();
    await act(async () => {
      renderBlock({ showAgents: true });
    });
    expect(screen.getByText("Sara - CTO")).toBeInTheDocument();
    // Archived agent is filtered out.
    expect(screen.queryByText("Old Bot")).not.toBeInTheDocument();
  });

  it("clicking an agent row opens that agent's chat", async () => {
    const onOpenAgentChat = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ showAgents: true, onOpenAgentChat });
    });

    await user.click(screen.getByText("Sara - CTO"));
    expect(onOpenAgentChat).toHaveBeenCalledWith("agent-sara");
  });

  it("toggles the agents setting from the control box", async () => {
    const onSetShowAgents = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ showAgents: false, onSetShowAgents });
    });

    await user.click(screen.getByText(/Show agents/));
    expect(onSetShowAgents).toHaveBeenCalledWith(true);
  });

  // TECH-3494 #3 — group by type shows headers; flat list shows none.
  it("groups by type with headers, or shows a flat list", async () => {
    await act(async () => {
      renderBlock({ groupBy: "type" });
    });
    expect(screen.getByText("Channels")).toBeInTheDocument();
    expect(screen.getByText("People")).toBeInTheDocument();

    cleanup();
    await act(async () => {
      renderBlock({ groupBy: "none" });
    });
    expect(screen.queryByText("Channels")).not.toBeInTheDocument();
    expect(screen.queryByText("People")).not.toBeInTheDocument();
    // Rows are still there, just ungrouped.
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("general")).toBeInTheDocument();
  });

  // TECH-3494 #1/#4 — one total limit across all kinds, no per-group scroll.
  it("caps the total rows across all kinds and has no inner scroll container", async () => {
    await act(async () => {
      renderBlock({ limit: 1, groupBy: "none", sort: "name" });
    });
    // Sorted by name: Alice first. Bob and the channel fall outside the cap.
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.queryByText("Bob")).not.toBeInTheDocument();
    expect(screen.queryByText("general")).not.toBeInTheDocument();
    // Old per-group scroll container is gone.
    expect(screen.queryByTestId("people-list")).not.toBeInTheDocument();
  });

  it("picking a limit from settings calls onSetLimit", async () => {
    const onSetLimit = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onSetLimit });
    });
    await user.click(screen.getByText("15"));
    expect(onSetLimit).toHaveBeenCalledWith(15);
  });

  it("filters the list from the search icon", async () => {
    const user = userEvent.setup();
    await act(async () => {
      renderBlock();
    });

    await user.click(screen.getByTitle("Search"));
    await user.type(
      screen.getByPlaceholderText("Search conversations..."),
      "bob",
    );

    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();
  });

  // Starred conversations float to the top and the star toggles the favorite.
  it("sorts a starred member above non-starred and toggles the star", async () => {
    favState.keys = ["member:bob"]; // Bob starred → above Alice
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ groupBy: "none" });
    });

    const names = screen
      .getAllByTestId(/^presence-dot-/)
      .map((d) => d.getAttribute("data-testid"));
    expect(names).toEqual(["presence-dot-bob", "presence-dot-alice"]);

    // Star Alice → toggle called with her actor key.
    const aliceRow = screen.getByText("Alice").closest(".group")!;
    await user.click(within(aliceRow as HTMLElement).getByLabelText("Star"));
    expect(favState.toggle).toHaveBeenCalledWith("member:alice");
  });

  it("renders the 'Chat' heading", async () => {
    await act(async () => {
      renderBlock();
    });
    expect(screen.getByText("Chat")).toBeInTheDocument();
  });

  it("picking a sort option from settings calls onSetSort", async () => {
    const onSetSort = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onSetSort });
    });
    await user.click(screen.getByText("Name"));
    expect(onSetSort).toHaveBeenCalledWith("name");
  });

  it("picking a group option from settings calls onSetGroupBy", async () => {
    const onSetGroupBy = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onSetGroupBy });
    });
    await user.click(screen.getByText("Flat list"));
    expect(onSetGroupBy).toHaveBeenCalledWith("none");
  });
});
