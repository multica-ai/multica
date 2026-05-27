// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import { I18nProvider } from "@multica/core/i18n/react";
import type {
  Agent,
  AgentAllowedPrincipal,
  MemberWithUser,
  UpdateAgentAllowedPrincipalsRequest,
} from "@multica/core/types";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockGetAgent = vi.hoisted(() => vi.fn());
const mockListAgents = vi.hoisted(() => vi.fn());
const mockListRuntimes = vi.hoisted(() => vi.fn());
const mockListMembers = vi.hoisted(() => vi.fn());
const mockListAgentAllowedPrincipals = vi.hoisted(() => vi.fn());
const mockUpdateAgentAllowedPrincipals = vi.hoisted(() => vi.fn());
const mockInspectorProps = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (state: { user: { id: string } | null }) => unknown) =>
    selector({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/paths", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/paths")>(
      "@multica/core/paths",
    );
  return {
    ...actual,
    useWorkspacePaths: () => actual.paths.workspace("test"),
  };
});

vi.mock("@multica/core/api", () => ({
  ApiError: class ApiError extends Error {
    status: number;
    constructor(status: number, message = "api error") {
      super(message);
      this.status = status;
    }
  },
  api: {
    getAgent: (...args: unknown[]) => mockGetAgent(...args),
    listAgents: (...args: unknown[]) => mockListAgents(...args),
    listRuntimes: (...args: unknown[]) => mockListRuntimes(...args),
    listMembers: (...args: unknown[]) => mockListMembers(...args),
    listAgentAllowedPrincipals: (...args: unknown[]) => mockListAgentAllowedPrincipals(...args),
    updateAgent: vi.fn(),
    updateAgentAllowedPrincipals: (...args: unknown[]) => mockUpdateAgentAllowedPrincipals(...args),
    archiveAgent: vi.fn(),
    restoreAgent: vi.fn(),
    createAgent: vi.fn(),
    setAgentSkills: vi.fn(),
  },
}));

vi.mock("@multica/core/agents", async () => {
  const actual =
    await vi.importActual<typeof import("@multica/core/agents")>(
      "@multica/core/agents",
    );
  return {
    ...actual,
    useWorkspacePresenceMap: () => ({ byAgent: new Map() }),
  };
});

vi.mock("@multica/core/permissions", () => ({
  useAgentPermissions: () => ({
    canEdit: { allowed: true, reason: "ok" },
  }),
}));

vi.mock("../../navigation", () => ({
  AppLink: ({
    href,
    children,
    ...rest
  }: {
    href: string;
    children: React.ReactNode;
    [k: string]: unknown;
  }) => (
    <a href={href} {...rest}>
      {children}
    </a>
  ),
  useNavigation: () => ({
    push: vi.fn(),
  }),
}));

vi.mock("./agent-detail-inspector", () => ({
  AgentDetailInspector: (props: {
    agent: Agent;
    allowedPrincipalUserIds: string[];
    onUpdateAllowedPrincipals: (
      data: UpdateAgentAllowedPrincipalsRequest,
    ) => Promise<void>;
  }) => {
    mockInspectorProps(props);
    return <div data-testid="agent-detail-inspector">{props.agent.instructions}</div>;
  },
}));

vi.mock("./agent-overview-pane", () => ({
  AgentOverviewPane: ({ agent }: { agent: Agent }) => (
    <div data-testid="agent-overview-pane">{agent.instructions}</div>
  ),
}));

vi.mock("./create-agent-dialog", () => ({
  CreateAgentDialog: () => null,
}));

vi.mock("../presence", () => ({
  availabilityConfig: {
    online: { textClass: "", dotClass: "" },
    offline: { textClass: "", dotClass: "" },
    busy: { textClass: "", dotClass: "" },
  },
}));

vi.mock("@multica/ui/components/common/capability-banner", () => ({
  CapabilityBanner: () => null,
}));

vi.mock("sonner", () => ({
  toast: {
    success: vi.fn(),
    error: vi.fn(),
  },
}));

import { AgentDetailPage } from "./agent-detail-page";

function makeAgent(overrides: Partial<Agent> = {}): Agent {
  return {
    id: "agent-1",
    workspace_id: "ws-1",
    runtime_id: "runtime-1",
    name: "Agent One",
    description: "",
    instructions: "",
    avatar_url: null,
    runtime_mode: "local",
    runtime_config: {},
    custom_env: {},
    custom_args: [],
    custom_env_redacted: false,
    visibility: "workspace",
    status: "idle",
    max_concurrent_tasks: 1,
    model: "",
    owner_id: "user-1",
    skills: [],
    created_at: "2026-05-25T00:00:00Z",
    updated_at: "2026-05-25T00:00:00Z",
    archived_at: null,
    archived_by: null,
    ...overrides,
  };
}

function makeMember(overrides: Partial<MemberWithUser> = {}): MemberWithUser {
  return {
    id: "member-1",
    workspace_id: "ws-1",
    user_id: "user-1",
    role: "owner",
    created_at: "2026-05-25T00:00:00Z",
    name: "Owner",
    email: "owner@example.com",
    avatar_url: null,
    ...overrides,
  };
}

function makeAllowedPrincipal(
  overrides: Partial<AgentAllowedPrincipal> = {},
): AgentAllowedPrincipal {
  return {
    id: "allowed-1",
    agent_id: "agent-1",
    user_id: "user-a",
    name: "Allowed A",
    email: "allowed-a@example.com",
    avatar_url: null,
    created_at: "2026-05-25T00:00:00Z",
    ...overrides,
  };
}

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <QueryClientProvider client={queryClient}>
        <AgentDetailPage agentId="agent-1" />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("AgentDetailPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListAgents.mockResolvedValue([makeAgent({ instructions: "" })]);
    mockGetAgent.mockResolvedValue(makeAgent({ instructions: "persisted instructions" }));
    mockListRuntimes.mockResolvedValue([]);
    mockListMembers.mockResolvedValue([makeMember()]);
    mockListAgentAllowedPrincipals.mockResolvedValue([]);
    mockUpdateAgentAllowedPrincipals.mockResolvedValue([]);
  });

  it("renders detail instructions instead of the slim list payload", async () => {
    renderPage();

    expect(await screen.findAllByText("persisted instructions")).toHaveLength(2);
  });

  it("updates allowed-principals cache from the save response", async () => {
    mockListAgents.mockResolvedValue([makeAgent({ visibility: "private" })]);
    mockGetAgent.mockResolvedValue(makeAgent({ visibility: "private" }));
    mockListAgentAllowedPrincipals
      .mockResolvedValueOnce([makeAllowedPrincipal({ user_id: "user-a" })])
      .mockResolvedValue([
        makeAllowedPrincipal({ user_id: "user-a" }),
        makeAllowedPrincipal({
          id: "allowed-2",
          user_id: "user-b",
          name: "Allowed B",
          email: "allowed-b@example.com",
        }),
      ]);
    mockUpdateAgentAllowedPrincipals.mockResolvedValue([
      makeAllowedPrincipal({ user_id: "user-a" }),
      makeAllowedPrincipal({
        id: "allowed-2",
        user_id: "user-b",
        name: "Allowed B",
        email: "allowed-b@example.com",
      }),
    ]);

    renderPage();

    await screen.findByTestId("agent-detail-inspector");
    await waitFor(() => {
      expect(
        mockInspectorProps.mock.calls.at(-1)?.[0].allowedPrincipalUserIds,
      ).toEqual(["user-a"]);
    });
    const props = mockInspectorProps.mock.calls.at(-1)?.[0];
    expect(props.allowedPrincipalUserIds).toEqual(["user-a"]);

    await props.onUpdateAllowedPrincipals({ add_user_ids: ["user-b"] });

    await waitFor(() => {
      expect(
        mockInspectorProps.mock.calls.at(-1)?.[0].allowedPrincipalUserIds,
      ).toEqual(["user-a", "user-b"]);
    });
    expect(mockUpdateAgentAllowedPrincipals).toHaveBeenCalledWith("agent-1", {
      add_user_ids: ["user-b"],
    });
  });
});
