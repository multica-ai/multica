// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import type { Agent } from "@multica/core/types";

const mockListSkills = vi.hoisted(() => vi.fn());
const mockRuntimeListOptions = vi.hoisted(() => vi.fn());
const mockRuntimeLocalSkillsOptions = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    listSkills: (...args: unknown[]) => mockListSkills(...args),
    setAgentSkills: vi.fn(),
  },
}));

vi.mock("@multica/core/runtimes", () => ({
  runtimeListOptions: (...args: unknown[]) => mockRuntimeListOptions(...args),
  runtimeLocalSkillsOptions: (...args: unknown[]) =>
    mockRuntimeLocalSkillsOptions(...args),
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
    mockRuntimeListOptions.mockReturnValue({
      queryKey: ["runtimes", "ws-1", "list"],
      queryFn: () =>
        Promise.resolve([
          {
            id: "runtime-1",
            workspace_id: "ws-1",
            daemon_id: "daemon-1",
            name: "Claude (MacBook)",
            runtime_mode: "local",
            provider: "claude",
            launch_header: "",
            status: "online",
            device_info: "",
            metadata: {},
            owner_id: "user-1",
            last_seen_at: null,
            created_at: "2026-04-16T00:00:00Z",
            updated_at: "2026-04-16T00:00:00Z",
          },
        ]),
    });
    mockRuntimeLocalSkillsOptions.mockReturnValue({
      queryKey: ["runtimes", "local-skills", "runtime-1"],
      queryFn: () =>
        Promise.resolve({
          supported: true,
          skills: [
            {
              key: "review-helper",
              name: "Review Helper",
              description: "Review pull requests",
              provider: "claude",
              source_path: "~/.claude/skills/review-helper",
              file_count: 2,
            },
          ],
        }),
    });
  });

  it("shows runtime local skills for local agents", async () => {
    renderSkillsTab();

    expect(await screen.findByText("Local Runtime Skills")).toBeInTheDocument();
    expect(await screen.findByText("Review Helper")).toBeInTheDocument();
    expect(screen.getByText("claude")).toBeInTheDocument();
    expect(screen.getByText("~/.claude/skills/review-helper")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Import to Workspace/i })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: /Import From Runtime/i })).not.toBeInTheDocument();

    await waitFor(() => {
      expect(mockRuntimeLocalSkillsOptions).toHaveBeenCalledWith("runtime-1");
    });
  });
});
