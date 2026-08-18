// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

// The DM tests exercise the header action wiring plus the real permission
// rules (via auth + member fixtures); the tabbed body and avatar/presence
// widgets are irrelevant weight, so they're stubbed.
vi.mock("./agent-overview-pane", () => ({
  AgentOverviewPane: ({
    agent,
    onUpdate,
  }: {
    agent: Agent;
    onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
  }) => (
    <button
      type="button"
      onClick={() => void onUpdate(agent.id, { model: "new-model" })}
    >
      update model
    </button>
  ),
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>actor-avatar</div>,
}));
vi.mock("./agent-presence-indicator", () => ({
  AgentPresenceIndicator: () => null,
}));

const agentsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
// When set, the member query never resolves — the "membership still loading"
// window in which the DM decision is undetermined.
const membersPendingRef = vi.hoisted(() => ({ current: false }));
const currentUserRef = vi.hoisted(() => ({
  current: { id: "user-1" } as { id: string } | null,
}));
const mockToastError = vi.hoisted(() => vi.fn());
const mockModalOpen = vi.hoisted(() => vi.fn());
const mockGetAgent = vi.hoisted(() => vi.fn());
const mockUpdateAgent = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: (agent: { runtime_id: string; runtime_bound?: boolean }) =>
    agent.runtime_bound !== false && agent.runtime_id.length > 0,
  useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  agentListOptions: (wsId: string) => ({
    queryKey: ["agents", wsId],
    queryFn: () => Promise.resolve(agentsRef.current),
  }),
  agentDetailOptions: (wsId: string, agentId: string) => ({
    queryKey: ["agents", wsId, "detail", agentId],
    queryFn: () => mockGetAgent(agentId),
    enabled: !!wsId && !!agentId,
    retry: false,
  }),
  cacheAgentResponse: (
    queryClient: QueryClient,
    wsId: string,
    agent: Agent,
    options: { insertIntoList?: boolean } = {},
  ) => {
    queryClient.setQueryData(["agents", wsId, "detail", agent.id], agent);
    queryClient.setQueryData<Agent[]>(["agents", wsId], (current) =>
      current?.some((item) => item.id === agent.id)
        ? current.map((item) => (item.id === agent.id ? agent : item))
        : current
          ? options.insertIntoList === false
            ? current
            : [...current, agent]
          : current,
    );
  },
  memberListOptions: (wsId: string) => ({
    queryKey: ["members", wsId],
    queryFn: () =>
      membersPendingRef.current
        ? new Promise(() => {})
        : Promise.resolve(membersRef.current),
  }),
  workspaceKeys: {
    agents: (wsId: string) => ["agents", wsId],
    agent: (wsId: string, agentId: string) => [
      "agents",
      wsId,
      "detail",
      agentId,
    ],
  },
}));
vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: (wsId: string) => ({
    queryKey: ["runtimes", wsId],
    queryFn: () => Promise.resolve([]),
  }),
}));
vi.mock("@multica/core/auth", () => {
  type AuthState = { user: { id: string } | null };
  const state = (): AuthState => ({ user: currentUserRef.current });
  const useAuthStore = Object.assign(
    (selector?: (s: AuthState) => unknown) =>
      selector ? selector(state()) : state(),
    { getState: state },
  );
  return { useAuthStore };
});
vi.mock("@multica/core/modals", () => ({
  useModalStore: Object.assign(vi.fn(), {
    getState: () => ({ open: mockModalOpen }),
  }),
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({
    agents: () => "/acme/agents",
    chat: () => "/acme/chat",
  }),
}));
vi.mock("@multica/core/api", () => {
  class ApiError extends Error {
    status: number;
    constructor(message: string, status: number) {
      super(message);
      this.status = status;
    }
  }
  return {
    api: { getAgent: mockGetAgent, updateAgent: mockUpdateAgent },
    ApiError,
  };
});
vi.mock("sonner", () => ({
  toast: { success: vi.fn(), error: mockToastError },
}));

import { AgentDetailPage } from "./agent-detail-page";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Lambda",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "local",
  runtime_config: {},
  custom_args: [],
  visibility: "workspace",
  permission_mode: "public_to",
  invocation_targets: [{ target_type: "workspace", target_id: null }],
  status: "idle",
  max_concurrent_tasks: 1,
  model: "",
  owner_id: "user-2",
  skills: [],
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const push = vi.fn();
  const navigation: NavigationAdapter = {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  const view = render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <AgentDetailPage agentId="agent-1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { push, queryClient, ...view };
}

beforeEach(() => {
  vi.clearAllMocks();
  currentUserRef.current = { id: "user-1" };
  membersRef.current = [{ user_id: "user-1", role: "member" }];
  membersPendingRef.current = false;
  agentsRef.current = [baseAgent];
  mockGetAgent.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
  mockUpdateAgent.mockResolvedValue({ ...baseAgent, model: "new-model" });
});

describe("AgentDetailPage direct-detail fallback", () => {
  it("does not fetch detail when the workspace list already has the agent", async () => {
    renderPage();

    expect(
      await screen.findByRole("button", { name: "Assign work" }),
    ).toBeInTheDocument();
    expect(mockGetAgent).not.toHaveBeenCalled();
  });

  it("keeps the loading skeleton while the detail request is pending", async () => {
    agentsRef.current = [];
    mockGetAgent.mockImplementation(() => new Promise(() => {}));

    const { container } = renderPage();

    await waitFor(() => expect(mockGetAgent).toHaveBeenCalledWith("agent-1"));
    expect(screen.queryByText("Agent not found")).not.toBeInTheDocument();
    expect(container.querySelector('[data-slot="skeleton"]')).not.toBeNull();
  });

  it("renders an agent returned by the detail endpoint", async () => {
    agentsRef.current = [];
    mockGetAgent.mockResolvedValue(baseAgent);

    renderPage();

    expect(
      await screen.findByRole("button", { name: "Assign work" }),
    ).toBeInTheDocument();
    expect(screen.queryByText("Agent not found")).not.toBeInTheDocument();
  });

  it("shows not found only after the detail endpoint returns 404", async () => {
    agentsRef.current = [];

    renderPage();

    expect(await screen.findByText("Agent not found")).toBeInTheDocument();
  });

  it("keeps 403 distinct from not found", async () => {
    agentsRef.current = [];
    mockGetAgent.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    renderPage();

    expect(
      await screen.findByText("You don't have access to this agent"),
    ).toBeInTheDocument();
    expect(screen.queryByText("Agent not found")).not.toBeInTheDocument();
  });

  it("shows 404 when a successful detail refetch later reports deletion", async () => {
    agentsRef.current = [];
    mockGetAgent.mockResolvedValueOnce(baseAgent);
    const { queryClient } = renderPage();
    await screen.findByRole("button", { name: "Assign work" });

    mockGetAgent.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: ["agents", "ws-1", "detail", "agent-1"],
        exact: true,
      });
    });

    expect(await screen.findByText("Agent not found")).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Assign work" }),
    ).not.toBeInTheDocument();
  });

  it("shows 403 when a successful detail refetch later loses access", async () => {
    agentsRef.current = [];
    mockGetAgent.mockResolvedValueOnce(baseAgent);
    const { queryClient } = renderPage();
    await screen.findByRole("button", { name: "Assign work" });

    mockGetAgent.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: ["agents", "ws-1", "detail", "agent-1"],
        exact: true,
      });
    });

    expect(
      await screen.findByText("You don't have access to this agent"),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: "Assign work" }),
    ).not.toBeInTheDocument();
  });

  it("shows a load failure instead of not found for transient errors", async () => {
    agentsRef.current = [];
    mockGetAgent.mockRejectedValue(new Error("Network request failed"));

    renderPage();

    expect(
      await screen.findByText("Couldn't load this agent"),
    ).toBeInTheDocument();
    expect(screen.getByText("Network request failed")).toBeInTheDocument();
    expect(screen.queryByText("Agent not found")).not.toBeInTheDocument();
  });

  it("keeps successful detail data visible through a transient refetch error", async () => {
    agentsRef.current = [];
    mockGetAgent.mockResolvedValueOnce(baseAgent);
    const { queryClient } = renderPage();
    await screen.findByRole("button", { name: "Assign work" });

    mockGetAgent.mockRejectedValue(new Error("Network request failed"));
    await act(async () => {
      await queryClient.invalidateQueries({
        queryKey: ["agents", "ws-1", "detail", "agent-1"],
        exact: true,
      });
    });

    expect(
      screen.getByRole("button", { name: "Assign work" }),
    ).toBeInTheDocument();
    expect(
      screen.queryByText("Couldn't load this agent"),
    ).not.toBeInTheDocument();
  });

  it("optimistically updates a detail-only cache entry", async () => {
    agentsRef.current = [];
    mockGetAgent.mockResolvedValue(baseAgent);
    mockUpdateAgent.mockImplementation(() => new Promise(() => {}));
    const { queryClient } = renderPage();
    await screen.findByRole("button", { name: "Assign work" });

    fireEvent.click(screen.getByRole("button", { name: "update model" }));

    expect(await screen.findByText("new-model")).toBeInTheDocument();
    expect(
      queryClient.getQueryData<Agent>([
        "agents",
        "ws-1",
        "detail",
        "agent-1",
      ]),
    ).toMatchObject({ model: "new-model" });
  });
});

describe("AgentDetailPage DM button", () => {
  it("navigates to the chat deep link when the user can chat with the agent", async () => {
    const { push } = renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "DM" }));
    expect(push).toHaveBeenCalledWith("/acme/chat?agent=agent-1");
    expect(mockToastError).not.toHaveBeenCalled();
  });

  it("shows a toast instead of navigating when the user lacks chat access", async () => {
    // Post-MUL-3963 a workspace admin can VIEW another member's private agent
    // but can no longer invoke (chat with) it — the exact case where the DM
    // button must explain itself rather than navigate.
    agentsRef.current = [
      { ...baseAgent, permission_mode: "private", invocation_targets: [] },
    ];
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    const { push } = renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "DM" }));
    expect(mockToastError).toHaveBeenCalledWith(
      "You don't have access to chat with this agent.",
    );
    expect(push).not.toHaveBeenCalled();
  });

  it("disables DM while membership is resolving instead of toasting a false deny", async () => {
    // Review P2: a pending member query collapses role to null, which the
    // rules read as not_member — a legitimate public_to+workspace member
    // would get a wrong "no access" toast. Undetermined must disable, not deny.
    membersPendingRef.current = true;
    const { push } = renderPage();
    // The control is an anchor now, so "disabled" is expressed the only way a
    // link can express it: aria-disabled plus removal from the tab order.
    const dm = await screen.findByRole("button", { name: "DM" });
    expect(dm).toHaveAttribute("aria-disabled", "true");
    expect(dm).toHaveAttribute("tabindex", "-1");
    fireEvent.click(dm);
    expect(mockToastError).not.toHaveBeenCalled();
    expect(push).not.toHaveBeenCalled();
  });

  it("hides the DM button on an archived agent", async () => {
    agentsRef.current = [
      { ...baseAgent, archived_at: "2026-06-01T00:00:00Z" },
    ];
    renderPage();
    // The archived banner is the signal the page has settled past loading.
    await screen.findByText(/This agent is archived/);
    expect(screen.queryByRole("button", { name: "DM" })).not.toBeInTheDocument();
  });

  it("hides the more-actions trigger when no menu actions are available", async () => {
    // The gate must survive a caller who genuinely CAN archive: an admin
    // passes `canEditAgent`, so `canArchive` is true. The only thing keeping
    // the menu empty is the system agent's undefined `onArchive`. Assert the
    // real aria label (locale `detail.more_actions_aria` = "Agent actions"),
    // so removing the `hasMoreActions` gate would render the empty shell and
    // fail this test.
    agentsRef.current = [
      { ...baseAgent, system_key: "mika" },
    ];
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    expect(
      screen.queryByLabelText("Agent actions"),
    ).not.toBeInTheDocument();
  });

  it("keeps the more-actions trigger for an editable non-system agent", async () => {
    // Positive counterpart: an owner of a normal agent has a real archive
    // action, so the menu trigger must still render. Guards the gate against
    // over-hiding.
    agentsRef.current = [{ ...baseAgent, owner_id: "user-1" }];
    membersRef.current = [{ user_id: "user-1", role: "member" }];
    renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    expect(
      screen.getByLabelText("Agent actions"),
    ).toBeInTheDocument();
  });

  it("explains an unbound agent and blocks run actions without losing the profile", async () => {
    agentsRef.current = [
      {
        ...baseAgent,
        owner_id: "user-1",
        runtime_id: "",
        runtime_bound: false,
      },
    ];
    renderPage();

    expect(
      await screen.findByText(/needs a runtime before it can run/i),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Bind runtime" }),
    ).toBeInTheDocument();

    fireEvent.click(screen.getByRole("button", { name: "DM" }));
    expect(mockToastError).toHaveBeenCalledWith(
      "Bind a runtime before running this agent.",
    );
    expect(mockModalOpen).not.toHaveBeenCalled();
  });
});
