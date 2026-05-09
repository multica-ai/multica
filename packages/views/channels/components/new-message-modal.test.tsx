import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, Channel, MemberWithUser } from "@multica/core/types";

const mockCreateChannel = vi.hoisted(() => vi.fn());

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
    user_id: "mads",
    role: "member",
    created_at: "",
    name: "Mads",
    email: "mads@example.com",
    avatar_url: null,
    budget_enforcement_enabled: false,
  },
];

const agents: Agent[] = [
  {
    id: "a1",
    name: "Reviewer",
    visibility: "public",
    archived_at: null,
    owner_id: null,
  } as unknown as Agent,
  {
    id: "a2",
    name: "Locked",
    visibility: "private",
    archived_at: null,
    owner_id: "someone-else",
  } as unknown as Agent,
];

vi.mock("@tanstack/react-query", async () => {
  const actual = await vi.importActual<typeof import("@tanstack/react-query")>(
    "@tanstack/react-query",
  );
  return {
    ...actual,
    useQuery: (options: { queryKey: readonly unknown[] }) => {
      const key = (options.queryKey?.[2] ?? options.queryKey?.[1]) as string;
      if (key === "members") return { data: members };
      if (key === "agents") return { data: agents };
      return { data: [] };
    },
  };
});

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({ queryKey: ["workspace", "ws", "members"] }),
  agentListOptions: () => ({ queryKey: ["workspace", "ws", "agents"] }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: { user: { id: string } }) => unknown) =>
    selector({ user: { id: "me" } }),
}));

vi.mock("@multica/core/workspace/hooks", () => ({
  useActorName: () => ({
    getActorName: (_t: string, id: string) => `User-${id}`,
    getActorInitials: () => "U",
    getActorAvatarUrl: () => null,
  }),
}));

// Stand-in for the favorites store. We expose `__setFavorites` so individual
// tests can seed pinned actors without persisting through localStorage.
const favoritesState = vi.hoisted(() => {
  return {
    favorites: [] as string[],
    listeners: new Set<() => void>(),
    setFavorites(next: string[]) {
      this.favorites = next;
      this.listeners.forEach((l) => l());
    },
    toggle(key: string) {
      this.setFavorites(
        this.favorites.includes(key)
          ? this.favorites.filter((k) => k !== key)
          : [...this.favorites, key],
      );
    },
  };
});

vi.mock("@multica/core/channels", () => {
  return {
    useCreateChannel: () => ({ mutateAsync: mockCreateChannel }),
  };
});

vi.mock("@multica/cerebro-channels", () => {
  const actorKey = (type: "member" | "agent", id: string) => `${type}:${id}`;
  const useChannelFavoritesStore = <T,>(
    selector: (s: { favorites: string[]; toggle: (k: string) => void }) => T,
  ): T => selector({ favorites: favoritesState.favorites, toggle: (k) => favoritesState.toggle(k) });
  return {
    useChannelFavoritesStore,
    actorKey,
  };
});

vi.mock("../../issues/components", () => ({
  // Mirrors the prod helper signature; private agents not owned by `me`
  // and not admin/owner are not assignable.
  canAssignAgent: (
    agent: Agent,
    userId: string | undefined,
    role: string | undefined,
  ) => {
    if (agent.visibility !== "private") return true;
    if (agent.owner_id === userId) return true;
    if (role === "owner" || role === "admin") return true;
    return false;
  },
}));

vi.mock("@multica/ui/components/ui/dialog", () => ({
  Dialog: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div data-testid="dialog">{children}</div> : null,
  DialogContent: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
  DialogTitle: ({ children }: { children: React.ReactNode }) => (
    <div>{children}</div>
  ),
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@multica/ui/components/ui/button", () => ({
  Button: ({
    children,
    disabled,
    onClick,
    type = "button",
  }: {
    children: React.ReactNode;
    disabled?: boolean;
    onClick?: () => void;
    type?: "button" | "submit" | "reset";
  }) => (
    <button type={type} disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

import { NewMessageModal } from "./new-message-modal";

const sampleChannel: Channel = {
  id: "ch1",
  workspace_id: "ws",
  number: 1,
  identifier: "JEH-1",
  kind: "dm",
  title: "",
  description: null,
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
  last_message: null,
  created_at: "",
  updated_at: "",
};

describe("NewMessageModal", () => {
  beforeEach(() => {
    mockCreateChannel.mockReset();
    mockCreateChannel.mockResolvedValue({ ...sampleChannel });
    favoritesState.setFavorites([]);
  });

  it("hides private agents the current user cannot access", () => {
    render(<NewMessageModal open onClose={() => {}} />);
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.queryByText("Locked")).not.toBeInTheDocument();
  });

  it("excludes the current user from the unified list", () => {
    render(<NewMessageModal open onClose={() => {}} />);
    expect(screen.queryByText("Me")).not.toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("shows agents and members in a single unified list (no section split)", () => {
    render(<NewMessageModal open onClose={() => {}} />);
    // Both kinds visible without switching tabs.
    expect(screen.getByText("Alice")).toBeInTheDocument();
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    // No agent/member section labels — only "Everyone" / "Favorites".
    expect(screen.queryByText("People")).not.toBeInTheDocument();
    expect(screen.queryByText("Agents")).not.toBeInTheDocument();
  });

  it("tapping a member immediately creates a DM (no extra confirm step)", async () => {
    const user = userEvent.setup();
    const onCreated = vi.fn();
    render(<NewMessageModal open onClose={() => {}} onCreated={onCreated} />);

    await user.click(screen.getByText("Alice"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        kind: "dm",
        name: "",
        member_ids: ["alice"],
        agent_ids: [],
      });
    });
    expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ id: "ch1" }));
  });

  it("tapping an agent dispatches to the chat flow instead of creating a channel", async () => {
    const user = userEvent.setup();
    const onAgentChatStarted = vi.fn();
    render(
      <NewMessageModal
        open
        onClose={() => {}}
        onAgentChatStarted={onAgentChatStarted}
      />,
    );

    await user.click(screen.getByText("Reviewer"));

    expect(onAgentChatStarted).toHaveBeenCalledWith("a1");
    expect(mockCreateChannel).not.toHaveBeenCalled();
  });

  it("falls back to a one-agent DM when no chat-start callback is provided", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    await user.click(screen.getByText("Reviewer"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        kind: "dm",
        name: "",
        member_ids: [],
        agent_ids: ["a1"],
      });
    });
  });

  it("group mode reveals checkboxes and creates a channel for multi-select", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    // Default mode: no group footer present.
    expect(screen.queryByText(/Pick people to add/)).not.toBeInTheDocument();

    await user.click(screen.getByRole("button", { name: /^Switch to channel$/ }));

    await user.click(screen.getByText("Alice"));
    await user.click(screen.getByText("Mads"));

    // Inline name input shows the auto-derived value and stays editable.
    expect(screen.getByDisplayValue("Alice, Mads")).toBeInTheDocument();

    await user.click(screen.getByText("Create channel (2)"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        kind: "channel",
        name: "Alice, Mads",
        member_ids: ["alice", "mads"],
        agent_ids: [],
      });
    });
  });

  it("group mode supports the user-edited channel name", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    await user.click(screen.getByRole("button", { name: /^Switch to channel$/ }));

    await user.click(screen.getByText("Alice"));
    await user.click(screen.getByText("Mads"));

    const nameInput = screen.getByDisplayValue("Alice, Mads");
    await user.clear(nameInput);
    await user.type(nameInput, "release-train");

    await user.click(screen.getByText("Create channel (2)"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith(
        expect.objectContaining({
          kind: "channel",
          name: "release-train",
        }),
      );
    });
  });

  it("group mode mixes members and agents into a single channel", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    await user.click(screen.getByRole("button", { name: /^Switch to channel$/ }));

    await user.click(screen.getByText("Alice"));
    await user.click(screen.getByText("Reviewer"));
    await user.click(screen.getByText("Create channel (2)"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        kind: "channel",
        name: "Alice, Reviewer",
        member_ids: ["alice"],
        agent_ids: ["a1"],
      });
    });
  });

  it("filters the list live by name", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    await user.type(screen.getByPlaceholderText("Search…"), "rev");

    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.queryByText("Alice")).not.toBeInTheDocument();
    expect(screen.queryByText("Mads")).not.toBeInTheDocument();
  });

  it("pins favorited actors to the top under a 'Favorites' section", () => {
    favoritesState.setFavorites(["agent:a1"]);
    render(<NewMessageModal open onClose={() => {}} />);

    expect(screen.getByText("Favorites")).toBeInTheDocument();
    expect(screen.getByText("Everyone")).toBeInTheDocument();
  });

  it("toggles favorite via the star button without selecting the actor", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} />);

    await user.click(screen.getByLabelText("Favorite Alice"));
    expect(favoritesState.favorites).toEqual(["member:alice"]);

    // Star toggle did not start a DM either.
    expect(mockCreateChannel).not.toHaveBeenCalled();
  });
});
