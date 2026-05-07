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

vi.mock("@multica/core/channels", () => ({
  useCreateChannel: () => ({ mutateAsync: mockCreateChannel }),
}));

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

vi.mock("@multica/ui/components/ui/tabs", () => ({
  Tabs: ({
    value,
    onValueChange,
    children,
  }: {
    value: string;
    onValueChange: (v: string) => void;
    children: React.ReactNode;
  }) => (
    <div data-testid="tabs" data-value={value} data-onchange={onValueChange.name}>
      {children}
      <button type="button" onClick={() => onValueChange("dm")}>
        switch-dm
      </button>
      <button type="button" onClick={() => onValueChange("channel")}>
        switch-channel
      </button>
    </div>
  ),
  TabsList: ({ children }: { children: React.ReactNode }) => <div>{children}</div>,
  TabsTrigger: ({ children, value }: { children: React.ReactNode; value: string }) => (
    <button type="button" data-tab={value}>
      {children}
    </button>
  ),
  TabsContent: ({
    value,
    children,
  }: {
    value: string;
    children?: React.ReactNode;
  }) => <div data-tab-content={value}>{children}</div>,
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
    mockCreateChannel.mockResolvedValue({ ...sampleChannel, kind: "dm" });
  });

  it("hides private agents the current user cannot access", () => {
    render(<NewMessageModal open onClose={() => {}} defaultTab="channel" />);
    expect(screen.getByText("Reviewer")).toBeInTheDocument();
    expect(screen.queryByText("Locked")).not.toBeInTheDocument();
  });

  it("does not show agents at all on the DM tab", () => {
    render(<NewMessageModal open onClose={() => {}} defaultTab="dm" />);
    expect(screen.queryByText("Reviewer")).not.toBeInTheDocument();
    expect(screen.getByText("Alice")).toBeInTheDocument();
  });

  it("excludes the current user from the people list", () => {
    render(<NewMessageModal open onClose={() => {}} defaultTab="dm" />);
    expect(screen.queryByText("Me")).not.toBeInTheDocument();
  });

  it("posts a DM with kind='dm' and exactly one peer (idempotency rules)", async () => {
    const user = userEvent.setup();
    const onCreated = vi.fn();
    render(
      <NewMessageModal open onClose={() => {}} onCreated={onCreated} defaultTab="dm" />,
    );
    await user.click(screen.getByText("Alice"));
    await user.click(screen.getByText("Start chat"));

    await waitFor(() => {
      expect(mockCreateChannel).toHaveBeenCalledWith({
        kind: "dm",
        name: "",
        description: undefined,
        member_ids: ["alice"],
        agent_ids: [],
      });
    });
    expect(onCreated).toHaveBeenCalledWith(expect.objectContaining({ id: "ch1" }));
  });

  it("requires a name and at least one participant before creating a channel", async () => {
    const user = userEvent.setup();
    render(<NewMessageModal open onClose={() => {}} defaultTab="channel" />);

    const submit = screen.getByText("Create channel");
    expect(submit).toBeDisabled();

    await user.type(screen.getByPlaceholderText("Channel name"), "release-train");
    expect(submit).toBeDisabled(); // still no participants

    await user.click(screen.getByText("Alice"));
    expect(submit).not.toBeDisabled();
  });
});
