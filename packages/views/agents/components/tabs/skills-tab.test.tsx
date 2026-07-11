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
const mockRemoveAgentSkill = vi.hoisted(() => vi.fn());
const mockRuntimeCapabilities = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    getSkill: (...args: unknown[]) => mockGetSkill(...args),
    setAgentSkills: vi.fn(),
    setAgentSkillEnabled: (...args: unknown[]) => mockSetAgentSkillEnabled(...args),
    removeAgentSkill: (...args: unknown[]) => mockRemoveAgentSkill(...args),
  },
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
    const runtime: AgentRuntime = {
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

    renderSkillsTab({}, runtime);

    expect(await screen.findByText("Local review")).toBeInTheDocument();
    expect(screen.getByText("Host-level review workflow")).toBeInTheDocument();
  });
});
