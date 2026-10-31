// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import {
  act,
  fireEvent,
  render,
  screen,
  waitFor,
  within,
} from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { ApiError } from "@multica/core/api";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import { RESOURCES } from "../../locales";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../navigation";

// The page-level tests exercise the header actions, the URL-driven dialogs and
// the real permission rules (via auth + member fixtures). Each body section,
// the properties panel and both dialogs have their own tests, so they're
// stubbed down to the props the page hands them.
vi.mock("./agent-properties-panel", () => ({
  AgentPropertiesPanel: ({
    agent,
    onUpdate,
    onEditAccess,
  }: {
    agent: Agent;
    onUpdate: (id: string, data: Record<string, unknown>) => Promise<void>;
    onEditAccess: () => void;
  }) => (
    <div>
      <span>model:{agent.model}</span>
      <button
        type="button"
        onClick={() => void onUpdate(agent.id, { model: "new-model" })}
      >
        update model
      </button>
      <button type="button" onClick={onEditAccess}>
        edit access
      </button>
    </div>
  ),
}));
vi.mock("./tabs/activity-tab", () => ({
  AgentWorkSections: ({ onViewAll }: { onViewAll: () => void }) => (
    <button type="button" onClick={onViewAll}>
      view all work
    </button>
  ),
}));
vi.mock("./agent-instructions-section", () => ({
  AgentInstructionsSection: () => <div>instructions-section</div>,
}));
vi.mock("./agent-skills-section", () => ({
  AgentSkillsSection: ({ onManage }: { onManage: () => void }) => (
    <button type="button" onClick={onManage}>
      manage skills
    </button>
  ),
}));
vi.mock("./agent-advanced-settings", () => ({
  isAdvancedSection: (value: unknown) =>
    typeof value === "string" &&
    [
      "general",
      "access",
      "env",
      "custom_args",
      "runtime_config",
      "skills",
      "mcp_config",
      "composio_mcp",
      "integrations",
    ].includes(value),
  AgentAdvancedSettings: ({
    section,
    onSectionChange,
  }: {
    section: string | null;
    onSectionChange: (section: string | null) => void;
  }) =>
    section === null ? null : (
      <div data-testid="advanced-settings">
        advanced:{section}
        <button type="button" onClick={() => onSectionChange(null)}>
          close advanced
        </button>
      </div>
    ),
}));
vi.mock("./agent-work-dialog", () => ({
  AgentWorkDialog: ({
    open,
    onOpenChange,
  }: {
    open: boolean;
    onOpenChange: (open: boolean) => void;
  }) =>
    open ? (
      <div data-testid="work-dialog">
        <button type="button" onClick={() => onOpenChange(false)}>
          close work
        </button>
      </div>
    ) : null,
}));
vi.mock("../../common/actor-avatar", () => ({
  ActorAvatar: () => <div>actor-avatar</div>,
}));

const agentsRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const membersRef = vi.hoisted(() => ({ current: [] as unknown[] }));
// When set, the member query never resolves — the "membership still loading"
// window in which the DM decision is undetermined.
const membersPendingRef = vi.hoisted(() => ({ current: false }));
const presenceRef = vi.hoisted(() => ({ current: new Map<string, unknown>() }));
const runtimesRef = vi.hoisted(() => ({ current: [] as unknown[] }));
const currentUserRef = vi.hoisted(() => ({
  current: { id: "user-1" } as { id: string } | null,
}));
const mockToastError = vi.hoisted(() => vi.fn());
const mockToastSuccess = vi.hoisted(() => vi.fn());
const mockModalOpen = vi.hoisted(() => vi.fn());
const mockGetAgent = vi.hoisted(() => vi.fn());
const mockUpdateAgent = vi.hoisted(() => vi.fn());
const mockArchiveAgent = vi.hoisted(() => vi.fn());
const mockCancelAgentTasks = vi.hoisted(() => vi.fn());
const mockCopyText = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/agents", () => ({
  isAgentRuntimeBound: (agent: { runtime_id: string; runtime_bound?: boolean }) =>
    agent.runtime_bound !== false && agent.runtime_id.length > 0,
  useWorkspacePresenceMap: () => ({ byAgent: presenceRef.current }),
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
    queryFn: () => Promise.resolve(runtimesRef.current),
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
    agentDetail: (id: string) => `/acme/agents/${id}`,
    newAgentManual: () => "/acme/agents/new/manual",
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
    api: {
      getAgent: mockGetAgent,
      updateAgent: mockUpdateAgent,
      archiveAgent: mockArchiveAgent,
      restoreAgent: vi.fn(),
      cancelAgentTasks: mockCancelAgentTasks,
    },
    ApiError,
  };
});
vi.mock("@multica/ui/lib/clipboard", () => ({
  copyText: mockCopyText,
}));
vi.mock("sonner", () => ({
  toast: { success: mockToastSuccess, error: mockToastError },
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

function renderPage(search = "") {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const push = vi.fn();
  const replace = vi.fn();
  const navigation: NavigationAdapter = {
    push,
    replace,
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(search),
    hash: "",
    getShareableUrl: (path) => `https://app.test${path}`,
  };
  const view = render(
    <I18nProvider locale="en" resources={RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <AgentDetailPage agentId="agent-1" />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
  return { push, replace, queryClient, ...view };
}

function openMoreActions() {
  fireEvent.click(screen.getByRole("button", { name: "Agent actions" }));
  return screen.getByRole("menu");
}

beforeEach(() => {
  vi.clearAllMocks();
  currentUserRef.current = { id: "user-1" };
  membersRef.current = [{ user_id: "user-1", role: "member" }];
  membersPendingRef.current = false;
  presenceRef.current = new Map();
  runtimesRef.current = [];
  agentsRef.current = [baseAgent];
  mockGetAgent.mockRejectedValue(new ApiError("not found", 404, "Not Found"));
  mockUpdateAgent.mockResolvedValue({ ...baseAgent, model: "new-model" });
  mockArchiveAgent.mockResolvedValue(undefined);
  mockCancelAgentTasks.mockResolvedValue({ cancelled: 2 });
  mockCopyText.mockResolvedValue(true);
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
    // The loading state already carries the page chrome.
    expect(
      within(screen.getByRole("banner")).getByRole("link", { name: "Agents" }),
    ).toHaveAttribute("href", "/acme/agents");
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

    expect(
      await screen.findByRole("heading", { name: "Agent not found" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Back to agents" }),
    ).toHaveAttribute("href", "/acme/agents");
  });

  it("keeps 403 distinct from not found", async () => {
    agentsRef.current = [];
    mockGetAgent.mockRejectedValue(
      new ApiError("forbidden", 403, "Forbidden"),
    );

    renderPage();

    expect(
      await screen.findByRole("heading", { name: "You don't have access to this agent" }),
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

    expect(
      await screen.findByRole("heading", { name: "Agent not found" }),
    ).toBeInTheDocument();
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
      await screen.findByRole("heading", { name: "You don't have access to this agent" }),
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
      await screen.findByRole("heading", { name: "Couldn't load this agent" }),
    ).toBeInTheDocument();
    expect(screen.getByText("Network request failed")).toBeInTheDocument();
    expect(screen.queryByText("Agent not found")).not.toBeInTheDocument();
  });

  it("retries a failed load from the error state", async () => {
    agentsRef.current = [];
    mockGetAgent.mockRejectedValueOnce(new Error("Network request failed"));
    mockGetAgent.mockResolvedValue(baseAgent);

    renderPage();

    fireEvent.click(await screen.findByRole("button", { name: "Try again" }));
    expect(
      await screen.findByRole("button", { name: "Assign work" }),
    ).toBeInTheDocument();
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

    expect(await screen.findByText("model:new-model")).toBeInTheDocument();
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

describe("AgentDetailPage layout", () => {
  it("puts the agent in the breadcrumb and its name and description in the body", async () => {
    agentsRef.current = [{ ...baseAgent, description: "Design and frontend" }];
    renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    const header = screen.getByRole("banner");
    expect(within(header).getByRole("link", { name: "Agents" })).toHaveAttribute(
      "href",
      "/acme/agents",
    );
    expect(within(header).getByText("Lambda")).toBeInTheDocument();
    expect(
      screen.getByRole("heading", { name: "Lambda", level: 1 }),
    ).toBeInTheDocument();
    expect(screen.getByText("Design and frontend")).toBeInTheDocument();
    expect(screen.getByText("instructions-section")).toBeInTheDocument();
    expect(screen.queryByRole("tablist")).not.toBeInTheDocument();
  });

  it("shows the Skills section, except on a Qoder Cloud Agents runtime", async () => {
    const { unmount } = renderPage();
    expect(
      await screen.findByRole("button", { name: "manage skills" }),
    ).toBeInTheDocument();
    unmount();

    runtimesRef.current = [{ id: "runtime-1", provider: "qoder_cloud" }];
    renderPage();
    await screen.findByRole("button", { name: "Assign work" });
    await waitFor(() =>
      expect(
        screen.queryByRole("button", { name: "manage skills" }),
      ).not.toBeInTheDocument(),
    );
    expect(screen.getByText("instructions-section")).toBeInTheDocument();
  });

  it("assigns work through the quick-create modal", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "Assign work" }));
    expect(mockModalOpen).toHaveBeenCalledWith("quick-create-issue", {
      agent_id: "agent-1",
    });
  });

  it("shows the presence dot on the avatar once presence is known", async () => {
    presenceRef.current = new Map([
      ["agent-1", { availability: "online", runningCount: 0, queuedCount: 0 }],
    ]);
    renderPage();
    expect(
      await screen.findByRole("img", { name: /Online/ }),
    ).toBeInTheDocument();
  });
});

describe("AgentDetailPage deep links", () => {
  it("opens Advanced settings at the section named by ?view=", async () => {
    renderPage("view=general&focus=conversation_starters");
    expect(await screen.findByTestId("advanced-settings")).toHaveTextContent(
      "advanced:general",
    );
  });

  it.each([
    ["tab=settings", "advanced:general"],
    ["tab=capabilities", "advanced:skills"],
    ["view=mcp_config", "advanced:mcp_config"],
  ])("maps the legacy link %s to %s", async (search, expected) => {
    renderPage(search);
    expect(await screen.findByTestId("advanced-settings")).toHaveTextContent(
      expected,
    );
  });

  it.each(["view=overview", "view=instructions", "tab=overview", "view=bogus"])(
    "renders the plain page for %s",
    async (search) => {
      renderPage(search);
      await screen.findByRole("button", { name: "Assign work" });
      expect(screen.queryByTestId("advanced-settings")).not.toBeInTheDocument();
      expect(screen.queryByTestId("work-dialog")).not.toBeInTheDocument();
    },
  );

  it("opens the full work list for ?view=work and clears the URL on close", async () => {
    const { replace } = renderPage("view=work");
    fireEvent.click(await screen.findByRole("button", { name: "close work" }));
    expect(screen.queryByTestId("work-dialog")).not.toBeInTheDocument();
    expect(replace).toHaveBeenCalledWith("/acme/agents/agent-1");
  });

  it("writes the open section to the URL, dropping a legacy ?tab=", async () => {
    const { replace } = renderPage("tab=activity");
    fireEvent.click(await screen.findByRole("button", { name: "manage skills" }));
    expect(screen.getByTestId("advanced-settings")).toHaveTextContent(
      "advanced:skills",
    );
    expect(replace).toHaveBeenLastCalledWith("/acme/agents/agent-1?view=skills");

    fireEvent.click(screen.getByRole("button", { name: "close advanced" }));
    expect(screen.queryByTestId("advanced-settings")).not.toBeInTheDocument();
    expect(replace).toHaveBeenLastCalledWith("/acme/agents/agent-1");
  });

  it("opens Access settings from the properties panel", async () => {
    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "edit access" }));
    expect(screen.getByTestId("advanced-settings")).toHaveTextContent(
      "advanced:access",
    );
  });

  it("opens the work list from View all", async () => {
    const { replace } = renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "view all work" }));
    expect(screen.getByTestId("work-dialog")).toBeInTheDocument();
    expect(replace).toHaveBeenLastCalledWith("/acme/agents/agent-1?view=work");
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
    expect(
      screen.queryByRole("button", { name: "Assign work" }),
    ).not.toBeInTheDocument();
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
    fireEvent.click(screen.getByRole("button", { name: "Bind runtime" }));
    expect(screen.getByTestId("advanced-settings")).toHaveTextContent(
      "advanced:general",
    );

    fireEvent.click(screen.getByRole("button", { name: "DM" }));
    expect(mockToastError).toHaveBeenCalledWith(
      "Bind a runtime before running this agent.",
    );
    fireEvent.click(screen.getByRole("button", { name: "Assign work" }));
    expect(mockToastError).toHaveBeenCalledTimes(2);
    expect(mockModalOpen).not.toHaveBeenCalled();
  });
});

describe("AgentDetailPage more actions", () => {
  it("keeps Advanced settings and copy actions but no Archive for a system agent", async () => {
    // An admin passes `canEditAgent`, so only the system key keeps Archive
    // out of the menu.
    agentsRef.current = [{ ...baseAgent, system_key: "mika" }];
    membersRef.current = [{ user_id: "user-1", role: "admin" }];
    renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    const menu = openMoreActions();
    expect(
      within(menu).getByRole("menuitem", { name: "Advanced settings" }),
    ).toBeInTheDocument();
    expect(
      within(menu).getByRole("menuitem", { name: "Copy link" }),
    ).toBeInTheDocument();
    expect(
      within(menu).queryByRole("menuitem", { name: "Archive Agent" }),
    ).not.toBeInTheDocument();
  });

  it("archives an owned agent after confirmation and returns to the list", async () => {
    agentsRef.current = [{ ...baseAgent, owner_id: "user-1" }];
    const { push } = renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    fireEvent.click(
      within(openMoreActions()).getByRole("menuitem", { name: "Archive Agent" }),
    );
    fireEvent.click(await screen.findByRole("button", { name: "Archive" }));

    expect(mockArchiveAgent).toHaveBeenCalledWith("agent-1");
    expect(push).toHaveBeenCalledWith("/acme/agents");
  });

  it("opens Advanced settings at General", async () => {
    renderPage();
    await screen.findByRole("button", { name: "Assign work" });
    fireEvent.click(
      within(openMoreActions()).getByRole("menuitem", { name: "Advanced settings" }),
    );
    expect(screen.getByTestId("advanced-settings")).toHaveTextContent(
      "advanced:general",
    );
  });

  it("copies the shareable link and the agent ID", async () => {
    renderPage();
    await screen.findByRole("button", { name: "Assign work" });

    fireEvent.click(
      within(openMoreActions()).getByRole("menuitem", { name: "Copy link" }),
    );
    await waitFor(() => expect(mockToastSuccess).toHaveBeenCalledWith("Link copied"));
    expect(mockCopyText).toHaveBeenCalledWith("https://app.test/acme/agents/agent-1");

    fireEvent.click(
      within(openMoreActions()).getByRole("menuitem", { name: "Copy agent ID" }),
    );
    await waitFor(() =>
      expect(mockToastSuccess).toHaveBeenCalledWith("Agent ID copied"),
    );
    expect(mockCopyText).toHaveBeenLastCalledWith("agent-1");
  });

  it("links Duplicate to the manual create flow", async () => {
    renderPage();
    await screen.findByRole("button", { name: "Assign work" });
    expect(
      within(openMoreActions()).getByRole("menuitem", { name: "Duplicate" }),
    ).toHaveAttribute("href", "/acme/agents/new/manual?duplicate=agent-1");
  });

  it("cancels active runs only after confirmation", async () => {
    agentsRef.current = [{ ...baseAgent, owner_id: "user-1" }];
    presenceRef.current = new Map([
      ["agent-1", { availability: "online", runningCount: 1, queuedCount: 1 }],
    ]);
    renderPage();

    await screen.findByRole("button", { name: "Assign work" });
    fireEvent.click(
      within(openMoreActions()).getByRole("menuitem", { name: /Cancel all/ }),
    );
    expect(mockCancelAgentTasks).not.toHaveBeenCalled();
    const dialog = await screen.findByRole("alertdialog");
    fireEvent.click(within(dialog).getByRole("button", { name: "Cancel all runs" }));
    await waitFor(() => expect(mockCancelAgentTasks).toHaveBeenCalledWith("agent-1"));
  });

  it("offers no run cancellation while nothing is active", async () => {
    agentsRef.current = [{ ...baseAgent, owner_id: "user-1" }];
    renderPage();
    await screen.findByRole("button", { name: "Assign work" });
    expect(
      within(openMoreActions()).queryByRole("menuitem", { name: /Cancel all/ }),
    ).not.toBeInTheDocument();
  });
});
