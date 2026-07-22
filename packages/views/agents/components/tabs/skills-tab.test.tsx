// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockListSkills = vi.hoisted(() => vi.fn());
const mockGetSkill = vi.hoisted(() => vi.fn());
const mockSetAgentSkillEnabled = vi.hoisted(() => vi.fn());
const mockSetAgentRuntimeSkillEnabled = vi.hoisted(() => vi.fn());
const mockRemoveAgentSkill = vi.hoisted(() => vi.fn());
const mockRuntimeCapabilities = vi.hoisted(() => vi.fn());

// ApiError mirrors the production export. The tab branches on
// `instanceof ApiError` for the 403 permission notice, so the class identity
// must match the one the mocked query rejects with.
const { ApiError } = vi.hoisted(() => {
  class ApiError extends Error {
    status: number;
    statusText: string;
    constructor(message: string, status: number, statusText: string) {
      super(message);
      this.status = status;
      this.statusText = statusText;
    }
  }
  return { ApiError };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    getSkill: (...args: unknown[]) => mockGetSkill(...args),
    setAgentSkills: vi.fn(),
    setAgentSkillEnabled: (...args: unknown[]) => mockSetAgentSkillEnabled(...args),
    setAgentRuntimeSkillEnabled: (...args: unknown[]) =>
      mockSetAgentRuntimeSkillEnabled(...args),
    removeAgentSkill: (...args: unknown[]) => mockRemoveAgentSkill(...args),
  },
  ApiError,
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeCapabilitiesOptions: (runtimeId: string | null) => ({
    queryKey: ["runtime-capabilities", runtimeId],
    queryFn: () => mockRuntimeCapabilities(runtimeId),
    enabled: Boolean(runtimeId),
    retry: false,
  }),
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { SkillsTab } from "./skills-tab";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
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
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const onlineRuntime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Codex (Mac)",
  runtime_mode: "local",
  provider: "codex",
  launch_header: "",
  status: "online",
  device_info: "Mac",
  metadata: {},
  owner_id: "user-1",
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-07-11T00:00:00Z",
  updated_at: "2026-07-11T00:00:00Z",
};

function renderSkillsTab(
  agentOverrides: Partial<Agent> = {},
  runtime: AgentRuntime | null = null,
) {
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
        <SkillsTab agent={{ ...agent, ...agentOverrides }} runtime={runtime} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("SkillsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSkills.mockResolvedValue([]);
    mockSetAgentSkillEnabled.mockResolvedValue(undefined);
    mockSetAgentRuntimeSkillEnabled.mockResolvedValue(undefined);
    mockRemoveAgentSkill.mockResolvedValue(undefined);
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [],
      supported: true,
      mcpServers: [],
      mcpSupported: true,
    });
  });

  it("separates workspace assignments from inherited runtime skills", async () => {
    renderSkillsTab();

    expect(
      await screen.findByText("Assigned to agent"),
    ).toBeInTheDocument();
    expect(screen.getByText("Inherited from runtime")).toBeInTheDocument();
    expect(screen.getByText(/Assign a local runtime/i)).toBeInTheDocument();
  });

  it("disables an assigned skill without removing it", async () => {
    const user = userEvent.setup();
    renderSkillsTab({
      skills: [
        {
          id: "skill-1",
          name: "Review changes",
          description: "Review a patch",
          enabled: true,
        },
      ],
    });

    await user.click(screen.getByRole("switch", { name: /Toggle Review changes/i }));

    expect(mockSetAgentSkillEnabled).toHaveBeenCalledWith(
      "agent-1",
      "skill-1",
      false,
    );
    expect(mockRemoveAgentSkill).not.toHaveBeenCalled();
  });

  it("shows inherited skills discovered from the assigned runtime", async () => {
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [
        {
          key: "local-review",
          name: "Local review",
          description: "Host-level review workflow",
          source_path: "~/.codex/skills/local-review",
          provider: "codex",
          root: "provider",
          file_count: 2,
        },
      ],
      supported: true,
      mcpServers: [],
      mcpSupported: true,
    });

    renderSkillsTab({}, onlineRuntime);

    expect(await screen.findByText("Local review")).toBeInTheDocument();
    expect(screen.getByText("Host-level review workflow")).toBeInTheDocument();
  });

  it("turns a controllable inherited skill off for this agent", async () => {
    const user = userEvent.setup();
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [
        {
          key: "local-review",
          name: "Local review",
          source_path: "~/.codex/skills/local-review",
          provider: "codex",
          root: "provider",
          can_disable: true,
          file_count: 1,
        },
      ],
      supported: true,
      mcpServers: [],
      mcpSupported: true,
    });

    renderSkillsTab({}, onlineRuntime);
    await user.click(
      await screen.findByRole("switch", {
        name: /Toggle inherited Local review/i,
      }),
    );

    expect(mockSetAgentRuntimeSkillEnabled).toHaveBeenCalledWith("agent-1", {
      runtime_id: "runtime-1",
      root: "provider",
      key: "local-review",
      name: "Local review",
      plugin: undefined,
      enabled: false,
    });
  });

  it("renders a persisted inherited-skill override as off", async () => {
    mockRuntimeCapabilities.mockResolvedValue({
      skills: [
        {
          key: "local-review",
          name: "Local review",
          source_path: "~/.codex/skills/local-review",
          provider: "codex",
          root: "provider",
          can_disable: true,
          file_count: 1,
        },
      ],
      supported: true,
      mcpServers: [],
      mcpSupported: true,
    });

    renderSkillsTab(
      {
        disabled_runtime_skills: [
          {
            runtime_id: "runtime-1",
            provider: "codex",
            root: "provider",
            key: "local-review",
          },
        ],
      },
      onlineRuntime,
    );

    expect(
      await screen.findByRole("switch", {
        name: /Toggle inherited Local review/i,
      }),
    ).not.toBeChecked();
  });

  it("shows a permission notice when capability discovery is forbidden", async () => {
    mockRuntimeCapabilities.mockRejectedValue(
      new ApiError("insufficient permissions", 403, "Forbidden"),
    );

    renderSkillsTab({}, onlineRuntime);

    expect(
      await screen.findByText(
        "You don't have permission to view this runtime's skills.",
      ),
    ).toBeInTheDocument();
  });

  it("shows a retry notice when capability discovery fails", async () => {
    mockRuntimeCapabilities.mockRejectedValue(
      new Error("daemon did not respond within 3 minutes"),
    );

    renderSkillsTab({}, onlineRuntime);

    expect(
      await screen.findByText("Couldn't discover runtime skills. Try again."),
    ).toBeInTheDocument();
  });
});
