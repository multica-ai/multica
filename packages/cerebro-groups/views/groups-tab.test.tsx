import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

// ---------- Hoisted mocks ----------

const mockListGroups = vi.hoisted(() => vi.fn());
const mockCreateGroup = vi.hoisted(() => vi.fn());
const mockDeleteGroup = vi.hoisted(() => vi.fn());
const mockUpdateGroup = vi.hoisted(() => vi.fn());
const mockListGroupMembers = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockAddGroupMember = vi.hoisted(() => vi.fn());
const mockRemoveGroupMember = vi.hoisted(() => vi.fn());
const mockListGroupCapabilities = vi.hoisted(() =>
  vi.fn().mockResolvedValue([]),
);
const mockSetGroupCapability = vi.hoisted(() => vi.fn());
const mockRemoveGroupCapability = vi.hoisted(() => vi.fn());
const mockListGroupRuntimes = vi.hoisted(() =>
  vi.fn().mockResolvedValue([]),
);
const mockAddGroupRuntime = vi.hoisted(() => vi.fn());
const mockRemoveGroupRuntime = vi.hoisted(() => vi.fn());
const mockListGroupAgents = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockAddGroupAgent = vi.hoisted(() => vi.fn());
const mockRemoveGroupAgent = vi.hoisted(() => vi.fn());
const mockListRuntimes = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockListAgents = vi.hoisted(() => vi.fn().mockResolvedValue([]));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
      createCerebroGroup: mockCreateGroup,
      updateCerebroGroup: mockUpdateGroup,
      deleteCerebroGroup: mockDeleteGroup,
      listCerebroGroupMembers: mockListGroupMembers,
      addCerebroGroupMember: mockAddGroupMember,
      removeCerebroGroupMember: mockRemoveGroupMember,
      listCerebroGroupCapabilities: mockListGroupCapabilities,
      setCerebroGroupCapability: mockSetGroupCapability,
      removeCerebroGroupCapability: mockRemoveGroupCapability,
      listCerebroGroupRuntimes: mockListGroupRuntimes,
      addCerebroGroupRuntime: mockAddGroupRuntime,
      removeCerebroGroupRuntime: mockRemoveGroupRuntime,
      listCerebroGroupAgents: mockListGroupAgents,
      addCerebroGroupAgent: mockAddGroupAgent,
      removeCerebroGroupAgent: mockRemoveGroupAgent,
      listRuntimes: mockListRuntimes,
      listAgents: mockListAgents,
    },
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: any) => any) => {
      const state = {
        user: {
          id: "user-owner",
          email: "owner@multica.test",
          name: "Owner User",
        },
      };
      return selector ? selector(state) : state;
    },
    { getState: () => ({}) },
  ),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

const memberList = [
  {
    id: "m-owner",
    user_id: "user-owner",
    name: "Owner User",
    email: "owner@multica.test",
    role: "owner",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
  {
    id: "m-1",
    user_id: "user-1",
    name: "Anne Larsen",
    email: "anne@multica.test",
    role: "member",
    avatar_url: null,
    workspace_id: "ws-1",
    created_at: "",
  },
];

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["memberList"],
    queryFn: () => Promise.resolve(memberList),
  }),
  agentListOptions: () => ({
    queryKey: ["agentList"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimeList"],
    queryFn: () => Promise.resolve([]),
  }),
}));

vi.mock("@multica/ui/components/common/actor-avatar", () => ({
  ActorAvatar: ({ name }: { name: string }) => (
    <span data-testid={"avatar-" + name}>av</span>
  ),
}));

const mockUseFeatureFlag = vi.hoisted(() => vi.fn(() => true));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mockUseFeatureFlag,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { GroupsTab } from "./groups-tab";

function renderTab() {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <GroupsTab />
    </QueryClientProvider>,
  );
}

describe("GroupsTab", () => {
  it("returns null when cerebro_groups_enabled is off", () => {
    mockUseFeatureFlag.mockReturnValueOnce(false);
    mockListGroups.mockResolvedValueOnce([]);
    const { container } = renderTab();
    expect(container.firstChild).toBeNull();
  });

  it("renders an empty state when no groups exist", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
    mockListGroups.mockResolvedValueOnce([]);
    renderTab();

    await waitFor(() =>
      expect(screen.getByText(/No groups yet/i)).toBeInTheDocument(),
    );
    // Admin sees the create form
    expect(screen.getByTestId("create-group-form")).toBeInTheDocument();
  });

  it("renders existing groups with member counts and the permissions section", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
    mockListGroups.mockResolvedValueOnce([
      {
        id: "g-1",
        workspace_id: "ws-1",
        name: "Engineering",
        description: "Backend + frontend",
        created_by: "user-owner",
        created_at: "2026-05-11T00:00:00Z",
        updated_at: "2026-05-11T00:00:00Z",
      },
    ]);
    mockListGroupMembers.mockResolvedValueOnce([
      {
        group_id: "g-1",
        user_id: "user-1",
        added_by: "user-owner",
        added_at: "2026-05-11T00:00:00Z",
        user_name: "Anne Larsen",
        user_email: "anne@multica.test",
        user_avatar_url: null,
      },
    ]);
    // Permissions queries return empty by default for this render-only check.
    mockListGroupCapabilities.mockResolvedValue([]);
    mockListGroupRuntimes.mockResolvedValue([]);
    mockListGroupAgents.mockResolvedValue([]);
    renderTab();

    await waitFor(() =>
      expect(screen.getByText("Engineering")).toBeInTheDocument(),
    );
    expect(screen.getByText("Backend + frontend")).toBeInTheDocument();
    await waitFor(() =>
      expect(screen.getByText("Anne Larsen")).toBeInTheDocument(),
    );
    // JEH-1009 permissions UI replaces the PR 1 placeholder.
    expect(screen.getByTestId("permissions-section")).toBeInTheDocument();
    expect(screen.getByTestId("capability-section")).toBeInTheDocument();
    expect(screen.getByTestId("runtime-allowlist")).toBeInTheDocument();
    expect(screen.getByTestId("agent-allowlist")).toBeInTheDocument();
  });

  it("creates a new group via the inline form", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
    mockListGroups.mockResolvedValueOnce([]);
    mockCreateGroup.mockResolvedValueOnce({
      id: "g-new",
      workspace_id: "ws-1",
      name: "Design",
      description: null,
      created_by: "user-owner",
      created_at: "",
      updated_at: "",
    });

    renderTab();

    await waitFor(() =>
      expect(screen.getByTestId("create-group-form")).toBeInTheDocument(),
    );

    const nameInput = screen.getByLabelText("Group name");
    fireEvent.change(nameInput, { target: { value: "Design" } });
    fireEvent.click(screen.getByRole("button", { name: /Create group/i }));

    await waitFor(() => {
      expect(mockCreateGroup).toHaveBeenCalledWith("ws-1", {
        name: "Design",
        description: "",
      });
    });
  });
});
