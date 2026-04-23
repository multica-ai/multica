// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";

const mockListSkills = vi.hoisted(() => vi.fn());
const mockListRuntimes = vi.hoisted(() => vi.fn());
const mockInitiateListLocalSkills = vi.hoisted(() => vi.fn());
const mockGetListLocalSkillsResult = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
  useAuthStore: () => ({ user: { id: "user-1" } }),
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    listRuntimes: (...args: unknown[]) => mockListRuntimes(...args),
    initiateListLocalSkills: (...args: unknown[]) => mockInitiateListLocalSkills(...args),
    getListLocalSkillsResult: (...args: unknown[]) => mockGetListLocalSkillsResult(...args),
    setAgentSkills: vi.fn(),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { SkillsTab } from "./skills-tab";

const localRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "Local Runtime",
  runtime_mode: "local" as const,
  provider: "docker",
  launch_header: "",
  status: "online" as const,
  device_info: "",
  metadata: {},
  owner_id: "user-1",
  last_seen_at: "2026-04-16T00:00:00Z",
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
};

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
  custom_env: {},
  custom_args: [],
  custom_env_redacted: false,
  visibility: "workspace",
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

function renderSkillsTab() {
  const queryClient = new QueryClient({
    defaultOptions: {
      queries: {
        retry: false,
      },
    },
  });

  return render(
    <QueryClientProvider client={queryClient}>
      <SkillsTab agent={agent} />
    </QueryClientProvider>,
  );
}

describe("SkillsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockListSkills.mockResolvedValue([]);
    mockListRuntimes.mockResolvedValue([localRuntime]);
    // Default: no local skills
    mockInitiateListLocalSkills.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [],
      supported: true,
    });
    mockGetListLocalSkillsResult.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [],
      supported: true,
    });
  });

  it("renders empty state when no skills are available", async () => {
    renderSkillsTab();

    // Top informational callout should render
    expect(
      await screen.findByText(/Local runtime skills are always available/i),
    ).toBeInTheDocument();

    // Empty state message
    expect(screen.getByText("No skills assigned")).toBeInTheDocument();
  });

  it("displays local runtime skills when available", async () => {
    const localSkill = {
      key: "skill-1",
      name: "Test Local Skill",
      description: "A test skill from runtime",
      source_path: "/skills/test",
      provider: "docker",
      file_count: 3,
    };

    mockInitiateListLocalSkills.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [localSkill],
      supported: true,
    });
    mockGetListLocalSkillsResult.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [localSkill],
      supported: true,
    });

    renderSkillsTab();

    // Should show local runtime skills section header
    await waitFor(() => {
      expect(screen.getByText("Local Runtime Skills")).toBeInTheDocument();
    });

    // Should show the skill name
    expect(screen.getByText("Test Local Skill")).toBeInTheDocument();
    expect(screen.getByText("A test skill from runtime")).toBeInTheDocument();
  });

  it("displays workspace skills when assigned", async () => {
    const workspaceSkill = {
      id: "ws-skill-1",
      workspace_id: "ws-1",
      name: "Test Workspace Skill",
      description: "A workspace skill",
      content: "",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-04-16T00:00:00Z",
      updated_at: "2026-04-16T00:00:00Z",
    };

    mockListSkills.mockResolvedValue([workspaceSkill]);

    const agentWithSkills = {
      ...agent,
      skills: [workspaceSkill],
    };

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <SkillsTab agent={agentWithSkills} />
      </QueryClientProvider>,
    );

    // Should show workspace skills section header
    await waitFor(() => {
      expect(screen.getByText("Workspace Skills")).toBeInTheDocument();
    });

    // Should show the skill name
    expect(screen.getByText("Test Workspace Skill")).toBeInTheDocument();
  });

  it("shows combined skill count in header when both types exist", async () => {
    const localSkill = {
      key: "skill-1",
      name: "Local Skill",
      description: "Local",
      source_path: "/skills/local",
      provider: "docker",
      file_count: 1,
    };

    const workspaceSkill = {
      id: "ws-skill-1",
      workspace_id: "ws-1",
      name: "Workspace Skill",
      description: "Workspace",
      content: "",
      config: {},
      files: [],
      created_by: "user-1",
      created_at: "2026-04-16T00:00:00Z",
      updated_at: "2026-04-16T00:00:00Z",
    };

    mockInitiateListLocalSkills.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [localSkill],
      supported: true,
    });
    mockGetListLocalSkillsResult.mockResolvedValue({
      id: "req-1",
      runtime_id: "runtime-1",
      status: "completed",
      skills: [localSkill],
      supported: true,
    });
    mockListSkills.mockResolvedValue([workspaceSkill]);

    const agentWithSkills = {
      ...agent,
      skills: [workspaceSkill],
    };

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <SkillsTab agent={agentWithSkills} />
      </QueryClientProvider>,
    );

    // Should show combined count in subtitle
    await waitFor(() => {
      expect(screen.getByText("1 workspace + 1 local skills available")).toBeInTheDocument();
    });
  });

  it("does not fetch local skills for non-local runtimes", async () => {
    const cloudRuntime = {
      ...localRuntime,
      runtime_mode: "cloud" as const,
    };

    mockListRuntimes.mockResolvedValue([cloudRuntime]);

    const cloudAgent = {
      ...agent,
      runtime_id: "runtime-cloud",
      runtime_mode: "cloud" as const,
    };

    const queryClient = new QueryClient({
      defaultOptions: {
        queries: { retry: false },
      },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <SkillsTab agent={cloudAgent} />
      </QueryClientProvider>,
    );

    // Should not initiate local skills fetch for cloud runtime
    await waitFor(() => {
      expect(mockInitiateListLocalSkills).not.toHaveBeenCalled();
    });
  });
});
