// JEH-1067 — Tests for the new Group detail page. Covers the
// runtime-dependency badges, the "Tilføj begge" dual-add prompt, and the
// capability-tooltips on create_runtime / create_agent.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockListGroups = vi.hoisted(() => vi.fn());
const mockUpdateGroup = vi.hoisted(() => vi.fn());
const mockDeleteGroup = vi.hoisted(() => vi.fn());
const mockListGroupMembers = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockListGroupCapabilities = vi.hoisted(() =>
  vi.fn().mockResolvedValue([]),
);
const mockListGroupRuntimes = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockListGroupAgents = vi.hoisted(() => vi.fn().mockResolvedValue([]));
const mockAddGroupRuntime = vi.hoisted(() => vi.fn().mockResolvedValue({}));
const mockAddGroupAgent = vi.hoisted(() => vi.fn().mockResolvedValue({}));
const mockRemoveGroupRuntime = vi.hoisted(() => vi.fn());
const mockRemoveGroupAgent = vi.hoisted(() => vi.fn());
const mockRemoveGroupMember = vi.hoisted(() => vi.fn());
const mockAddGroupMember = vi.hoisted(() => vi.fn());
const mockSetGroupCapability = vi.hoisted(() => vi.fn());
const mockRemoveGroupCapability = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      listCerebroGroups: mockListGroups,
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

vi.mock("@multica/core/chat", () => ({
  useChatStore: (selector: (s: { setHideFloatingChat: (hide: boolean) => void }) => unknown) =>
    selector({ setHideFloatingChat: vi.fn() }),
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

const agentPayload = vi.hoisted<{ current: unknown[] }>(() => ({
  current: [],
}));
const runtimePayload = vi.hoisted<{ current: unknown[] }>(() => ({
  current: [],
}));

vi.mock("@multica/core/workspace/queries", () => ({
  memberListOptions: () => ({
    queryKey: ["memberList"],
    queryFn: () => Promise.resolve(memberList),
  }),
  agentListOptions: () => ({
    queryKey: ["agentList"],
    queryFn: () => Promise.resolve(agentPayload.current),
  }),
}));

vi.mock("@multica/core/runtimes/queries", () => ({
  runtimeListOptions: () => ({
    queryKey: ["runtimeList"],
    queryFn: () => Promise.resolve(runtimePayload.current),
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
  toast: { success: vi.fn(), error: vi.fn() },
}));

import { GroupDetailView } from "./group-detail";

const baseGroup = {
  id: "g-1",
  workspace_id: "ws-1",
  name: "Engineering",
  description: "Backend + frontend",
  created_by: "user-owner",
  created_at: "",
  updated_at: "",
};

function renderDetail(onBack: () => void = () => undefined) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <GroupDetailView groupId="g-1" onBack={onBack} />
    </QueryClientProvider>,
  );
}

describe("GroupDetailView", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListGroups.mockResolvedValue([baseGroup]);
    mockListGroupMembers.mockResolvedValue([]);
    mockListGroupCapabilities.mockResolvedValue([]);
    mockListGroupRuntimes.mockResolvedValue([]);
    mockListGroupAgents.mockResolvedValue([]);
    agentPayload.current = [];
    runtimePayload.current = [];
  });

  it("renders the four sections", async () => {
    renderDetail();
    await waitFor(() =>
      expect(screen.getByTestId("group-detail")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("members-section")).toBeInTheDocument();
    expect(screen.getByTestId("agents-section")).toBeInTheDocument();
    expect(screen.getByTestId("runtimes-section")).toBeInTheDocument();
    expect(screen.getByTestId("capabilities-section")).toBeInTheDocument();
  });

  it("renders the missing-group fallback when group is unknown", async () => {
    mockListGroups.mockResolvedValue([]);
    renderDetail();
    await waitFor(() =>
      expect(screen.getByTestId("group-detail-missing")).toBeInTheDocument(),
    );
  });

  it("calls onBack when the back button is clicked", async () => {
    const onBack = vi.fn();
    renderDetail(onBack);
    const back = await screen.findByTestId("group-detail-back");
    fireEvent.click(back);
    expect(onBack).toHaveBeenCalledTimes(1);
  });

  it("shows a runtime-dependency badge on each agent row", async () => {
    runtimePayload.current = [
      {
        id: "rt-1",
        workspace_id: "ws-1",
        daemon_id: null,
        name: "Claude Dev",
        runtime_mode: "cloud",
        provider: "anthropic",
        launch_header: "",
        status: "online",
        device_info: "",
        metadata: {},
        owner_id: null,
        sandbox_enabled: null,
        persona_sandbox: "",
        capabilities: {},
        last_seen_at: null,
        created_at: "",
        updated_at: "",
      },
    ];
    agentPayload.current = [
      {
        id: "agent-1",
        workspace_id: "ws-1",
        runtime_id: "rt-1",
        name: "Kristian",
        description: "",
        instructions: "",
        avatar_url: null,
        runtime_mode: "cloud",
        runtime_config: {},
        custom_env: {},
        custom_args: [],
        custom_env_redacted: false,
        visibility: "workspace",
        status: "idle",
        max_concurrent_tasks: 1,
        model: "claude",
        owner_id: null,
        skills: [],
        persona_sandbox: "",
        created_at: "",
        updated_at: "",
        archived_at: null,
        archived_by: null,
      },
    ];
    mockListGroupAgents.mockResolvedValue([
      {
        group_id: "g-1",
        agent_id: "agent-1",
        granted_by: "user-owner",
        granted_at: "",
      },
    ]);
    mockListGroupRuntimes.mockResolvedValue([
      {
        group_id: "g-1",
        runtime_id: "rt-1",
        granted_by: "user-owner",
        granted_at: "",
      },
    ]);

    renderDetail();

    await waitFor(() =>
      expect(screen.getByTestId("agent-runtime-badge")).toBeInTheDocument(),
    );
    expect(screen.getByTestId("agent-runtime-badge").textContent).toMatch(
      /Claude Dev/,
    );
    // Runtime is in allowlist → no "missing runtime" warning.
    expect(
      screen.queryByTestId("agent-runtime-missing-badge"),
    ).not.toBeInTheDocument();
  });

  it("prompts to add the runtime when picking an agent without it", async () => {
    runtimePayload.current = [
      {
        id: "rt-1",
        workspace_id: "ws-1",
        daemon_id: null,
        name: "Claude Dev",
        runtime_mode: "cloud",
        provider: "anthropic",
        launch_header: "",
        status: "online",
        device_info: "",
        metadata: {},
        owner_id: null,
        sandbox_enabled: null,
        persona_sandbox: "",
        capabilities: {},
        last_seen_at: null,
        created_at: "",
        updated_at: "",
      },
    ];
    agentPayload.current = [
      {
        id: "agent-1",
        workspace_id: "ws-1",
        runtime_id: "rt-1",
        name: "Kristian",
        description: "",
        instructions: "",
        avatar_url: null,
        runtime_mode: "cloud",
        runtime_config: {},
        custom_env: {},
        custom_args: [],
        custom_env_redacted: false,
        visibility: "workspace",
        status: "idle",
        max_concurrent_tasks: 1,
        model: "claude",
        owner_id: null,
        skills: [],
        persona_sandbox: "",
        created_at: "",
        updated_at: "",
        archived_at: null,
        archived_by: null,
      },
    ];
    // Group has no agents and no runtimes yet — picking the agent should
    // surface the dual-add prompt.
    mockListGroupAgents.mockResolvedValue([]);
    mockListGroupRuntimes.mockResolvedValue([]);

    renderDetail();

    const addAgentButton = await screen.findByRole("button", {
      name: /Add agent/i,
    });
    fireEvent.click(addAgentButton);

    const pickerAdd = await screen.findByRole("button", { name: "Add" });
    fireEvent.click(pickerAdd);

    await waitFor(() =>
      expect(screen.getByTestId("add-runtime-prompt")).toBeInTheDocument(),
    );
    expect(
      screen.getByText(/Denne agent kræver runtime/i),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: /Tilføj begge/i }));

    await waitFor(() => {
      expect(mockAddGroupRuntime).toHaveBeenCalledWith("g-1", "rt-1");
      expect(mockAddGroupAgent).toHaveBeenCalledWith("g-1", "agent-1");
    });
  }, 10_000);

  it("exposes capability tooltips for create_runtime and create_agent", async () => {
    renderDetail();
    await waitFor(() =>
      expect(
        screen.getByTestId("capability-create-runtime"),
      ).toBeInTheDocument(),
    );
    expect(
      screen.getByTestId("capability-create-agent"),
    ).toBeInTheDocument();
    // The tooltip copy is rendered as the description text under the toggle
    // (visible without hovering), so we can assert directly.
    expect(
      screen.getByText(/automatisk adgang til runtimes/i),
    ).toBeInTheDocument();
    expect(
      screen.getByText(/automatisk adgang til agenter/i),
    ).toBeInTheDocument();
  });
});
