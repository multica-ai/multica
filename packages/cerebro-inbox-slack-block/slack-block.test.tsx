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
    onSetMaxPeople: () => {},
    onSetSort: () => {},
    onRemove: () => {},
    onMove: () => {},
    isFirst: true,
    isLast: true,
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

  it("shows 'skriver…' when a typing event arrives for a member's selected DM", async () => {
    await act(async () => {
      renderBlock({ selectedChannelId: "dm-alice" });
    });

    expect(screen.queryByText("skriver…")).not.toBeInTheDocument();

    await act(async () => {
      wsHandlers.get("cerebro:typing")?.({
        channel_id: "dm-alice",
        user_id: "alice",
      });
    });

    expect(screen.getByText("skriver…")).toBeInTheDocument();
  });

  // #2 — the control box header with reorder + remove controls.
  it("renders the control box (move up/down + remove)", async () => {
    const onRemove = vi.fn();
    const onMove = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onRemove, onMove, isFirst: false, isLast: false });
    });

    await user.click(screen.getByTitle("Flyt op"));
    expect(onMove).toHaveBeenCalledWith(-1);
    await user.click(screen.getByTitle("Fjern blok"));
    expect(onRemove).toHaveBeenCalled();
  });

  // #3 — max-people setting caps the visible height while keeping the rest scrollable.
  it("makes the people list scrollable at maxPeople instead of hiding people", async () => {
    await act(async () => {
      renderBlock({ maxPeople: 1 });
    });

    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.getByTestId("people-list")).toHaveStyle({ maxHeight: "40px" });
    expect(screen.queryByText(/flere skjult/)).not.toBeInTheDocument();
  });

  it("picking a people limit from settings calls onSetMaxPeople", async () => {
    const onSetMaxPeople = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onSetMaxPeople });
    });
    await user.click(screen.getByText("10"));
    expect(onSetMaxPeople).toHaveBeenCalledWith(10);
  });

  it("filters people from the search icon", async () => {
    const user = userEvent.setup();
    await act(async () => {
      renderBlock();
    });

    await user.click(screen.getByTitle("Søg personer"));
    await user.type(screen.getByPlaceholderText("Søg personer..."), "bob");

    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();
    expect(screen.getByText("1/2")).toBeInTheDocument();
  });

  // #4 — starred people float to the top and the star toggles the favorite.
  it("sorts a starred member above non-starred and toggles the star", async () => {
    favState.keys = ["member:bob"]; // Bob starred → above Alice
    const user = userEvent.setup();
    await act(async () => {
      renderBlock();
    });

    const names = screen
      .getAllByTestId(/^presence-dot-/)
      .map((d) => d.getAttribute("data-testid"));
    expect(names).toEqual(["presence-dot-bob", "presence-dot-alice"]);

    // Star Alice → toggle called with her actor key.
    await user.click(screen.getAllByLabelText("Stjernemarkér")[0]!);
    expect(favState.toggle).toHaveBeenCalledWith("member:alice");
  });

  // TECH-3422 feedback #1 — the block heading reads "Chat".
  it("renders the 'Chat' heading", async () => {
    await act(async () => {
      renderBlock();
    });
    expect(screen.getByText("Chat")).toBeInTheDocument();
  });

  // TECH-3422 feedback #2 — a sort setting that drives onSetSort.
  it("picking a sort option from settings calls onSetSort", async () => {
    const onSetSort = vi.fn();
    const user = userEvent.setup();
    await act(async () => {
      renderBlock({ onSetSort });
    });
    await user.click(screen.getByText("Seneste aktivitet"));
    expect(onSetSort).toHaveBeenCalledWith("recent");
  });
});
