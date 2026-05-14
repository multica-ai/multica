// JEH-1067 — Groups list table tests. The legacy card-layout tests (which
// asserted on permissions-section/capability-section/etc.) moved to
// group-detail.test.tsx; those sub-components now live in GroupDetailView.

import { describe, it, expect, vi } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockListGroups = vi.hoisted(() => vi.fn());
const mockCreateGroup = vi.hoisted(() => vi.fn());
const mockListGroupMembers = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockListGroupCapabilities = vi.hoisted(() =>
  vi.fn().mockResolvedValue([]),
);
const mockListGroupRuntimes = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockListGroupAgents = vi.hoisted(() => vi.fn().mockResolvedValue([]));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
      createCerebroGroup: mockCreateGroup,
      listCerebroGroupMembers: mockListGroupMembers,
      listCerebroGroupCapabilities: mockListGroupCapabilities,
      listCerebroGroupRuntimes: mockListGroupRuntimes,
      listCerebroGroupAgents: mockListGroupAgents,
    },
  };
});

vi.mock("@multica/core/auth", () => ({
  useAuthStore: Object.assign(
    (selector?: (s: any) => any) => {
      const state = {
        user: { id: "user-owner", email: "owner@multica.test", name: "Owner" },
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
];

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["memberList"],
    queryFn: () => Promise.resolve(memberList),
  }),
}));

const mockUseFeatureFlag = vi.hoisted(() => vi.fn(() => true));
vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mockUseFeatureFlag,
}));

vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { GroupsTab } from "./groups-tab";

function renderTab(props: { onSelectGroup?: (id: string) => void } = {}) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <GroupsTab onSelectGroup={props.onSelectGroup} />
    </QueryClientProvider>,
  );
}

describe("GroupsTab (list)", () => {
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
    expect(screen.getByTestId("create-group-form")).toBeInTheDocument();
  });

  it("renders the new groups table with counts and capability badges", async () => {
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
        added_at: "",
        user_name: "Anne",
        user_email: "anne@multica.test",
        user_avatar_url: null,
      },
    ]);
    mockListGroupCapabilities.mockResolvedValueOnce([
      {
        group_id: "g-1",
        capability: "create_agent",
        granted_by: "user-owner",
        granted_at: "",
      },
    ]);
    mockListGroupRuntimes.mockResolvedValueOnce([]);
    mockListGroupAgents.mockResolvedValueOnce([]);

    renderTab();

    await waitFor(() =>
      expect(screen.getByTestId("groups-table")).toBeInTheDocument(),
    );
    expect(screen.getByText("Engineering")).toBeInTheDocument();
    expect(screen.getByText("Backend + frontend")).toBeInTheDocument();
    // Created-by name resolves via workspace members.
    expect(screen.getByText("Owner User")).toBeInTheDocument();
    await waitFor(() =>
      expect(
        screen.getByTestId("group-cap-badge-create-agent"),
      ).toBeInTheDocument(),
    );
  });

  it("invokes onSelectGroup when a row is clicked", async () => {
    mockUseFeatureFlag.mockReturnValue(true);
    mockListGroups.mockResolvedValueOnce([
      {
        id: "g-42",
        workspace_id: "ws-1",
        name: "Ops",
        description: null,
        created_by: null,
        created_at: "",
        updated_at: "",
      },
    ]);
    mockListGroupMembers.mockResolvedValue([]);
    mockListGroupCapabilities.mockResolvedValue([]);
    mockListGroupRuntimes.mockResolvedValue([]);
    mockListGroupAgents.mockResolvedValue([]);

    const onSelectGroup = vi.fn();
    renderTab({ onSelectGroup });

    const row = await screen.findByTestId("group-row");
    fireEvent.click(row);
    expect(onSelectGroup).toHaveBeenCalledWith("g-42");
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

    fireEvent.change(screen.getByLabelText("Group name"), {
      target: { value: "Design" },
    });
    fireEvent.click(screen.getByRole("button", { name: /Opret gruppe/i }));

    await waitFor(() => {
      expect(mockCreateGroup).toHaveBeenCalledWith("ws-1", {
        name: "Design",
        description: "",
      });
    });
  });
});
