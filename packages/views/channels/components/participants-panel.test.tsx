import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, Channel, MemberWithUser } from "@multica/core/types";

const mockToggle = vi.hoisted(() => vi.fn());

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

const agents: Agent[] = [
  {
    id: "a1",
    name: "Reviewer",
    visibility: "public",
    archived_at: null,
    owner_id: null,
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

vi.mock("@multica/core/channels", () => ({
  useToggleChannelParticipant: () => ({ mutate: mockToggle }),
}));

vi.mock("../../issues/components", () => ({
  canAssignAgent: () => true,
}));

vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: ({ actorId }: { actorId: string }) => (
    <span data-testid={`avatar-${actorId}`} />
  ),
}));

vi.mock("@multica/ui/components/ui/sheet", () => ({
  Sheet: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div data-testid="sheet">{children}</div> : null,
  SheetContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SheetHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  SheetTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
  SheetDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
}));

vi.mock("@multica/ui/components/ui/input", () => ({
  Input: (props: React.InputHTMLAttributes<HTMLInputElement>) => <input {...props} />,
}));

vi.mock("@multica/ui/components/ui/alert-dialog", () => ({
  AlertDialog: ({ open, children }: { open: boolean; children: React.ReactNode }) =>
    open ? <div data-testid="alert-dialog">{children}</div> : null,
  AlertDialogAction: ({
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
  AlertDialogCancel: ({ children }: { children: React.ReactNode }) => (
    <button type="button">{children}</button>
  ),
  AlertDialogContent: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogDescription: ({ children }: { children: React.ReactNode }) => <p>{children}</p>,
  AlertDialogFooter: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogHeader: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  AlertDialogTitle: ({ children }: { children: React.ReactNode }) => <h2>{children}</h2>,
}));

vi.mock("sonner", () => ({
  toast: { error: vi.fn() },
}));

import { ParticipantsPanel } from "./participants-panel";

const channel: Channel = {
  id: "c1",
  workspace_id: "ws",
  number: 1,
  identifier: "JEH-1",
  kind: "channel",
  title: "growth",
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

describe("ParticipantsPanel", () => {
  beforeEach(() => {
    mockToggle.mockReset();
  });

  it("lists current participants and excludes them from the add picker", () => {
    render(
      <ParticipantsPanel channel={channel} open onOpenChange={() => {}} />,
    );
    // Existing participants are listed.
    expect(screen.getAllByTestId("avatar-me").length).toBeGreaterThan(0);
    expect(screen.getAllByTestId("avatar-alice").length).toBeGreaterThan(0);
    // Add picker shows non-participants only.
    expect(screen.getByText("Bob")).toBeInTheDocument();
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
  });

  it("adds a participant via the picker", async () => {
    const user = userEvent.setup();
    render(
      <ParticipantsPanel channel={channel} open onOpenChange={() => {}} />,
    );
    await user.click(screen.getByText("Bob"));
    expect(mockToggle).toHaveBeenCalledWith(
      {
        channelId: "c1",
        member: { user_type: "member", user_id: "bob" },
        action: "add",
      },
      expect.any(Object),
    );
  });

  it("requires confirmation before removing a participant", async () => {
    const user = userEvent.setup();
    render(
      <ParticipantsPanel channel={channel} open onOpenChange={() => {}} />,
    );
    await user.click(screen.getByLabelText("Remove User-alice"));
    // Confirmation dialog opens; clicking 'Remove' fires the mutation.
    expect(screen.getByTestId("alert-dialog")).toBeInTheDocument();
    await user.click(screen.getByRole("button", { name: "Remove" }));
    expect(mockToggle).toHaveBeenCalledWith(
      {
        channelId: "c1",
        member: { user_type: "member", user_id: "alice" },
        action: "remove",
      },
      expect.any(Object),
    );
  });

  it("does not show remove buttons or add picker for DMs", () => {
    const dm: Channel = { ...channel, kind: "dm" };
    render(<ParticipantsPanel channel={dm} open onOpenChange={() => {}} />);
    expect(screen.queryByLabelText("Remove User-alice")).not.toBeInTheDocument();
    expect(screen.queryByText("Add participants")).not.toBeInTheDocument();
  });

  it("does not let the current user remove themselves", () => {
    render(
      <ParticipantsPanel channel={channel} open onOpenChange={() => {}} />,
    );
    expect(screen.queryByLabelText("Remove User-me")).not.toBeInTheDocument();
  });
});
