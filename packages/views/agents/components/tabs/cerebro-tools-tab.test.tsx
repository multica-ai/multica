// @vitest-environment jsdom
// CEREBRO-PATCH(agent-tools-tab-test): JEH-1359/1360 — QA regression coverage.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockGetAgentTools = vi.hoisted(() => vi.fn());
const mockUpdateAgentTool = vi.hoisted(() => vi.fn());
const mockUseQuery = vi.hoisted(() => vi.fn());
const mockInvalidateQueries = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-query", () => ({
  useQuery: (options: unknown) => mockUseQuery(options),
  useQueryClient: () => ({
    invalidateQueries: mockInvalidateQueries,
  }),
}));

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAgentTools: (...args: unknown[]) => mockGetAgentTools(...args),
    updateAgentTool: (...args: unknown[]) => mockUpdateAgentTool(...args),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { CerebroToolsTab } from "./cerebro-tools-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Agent",
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
  model: "",
  owner_id: "user-1",
  skills: [],
  created_at: "2026-04-16T00:00:00Z",
  updated_at: "2026-04-16T00:00:00Z",
  archived_at: null,
  archived_by: null,
  persona_sandbox: "",
};

function renderToolsTab(agent: Agent = baseAgent) {
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <CerebroToolsTab agent={agent} />
    </I18nProvider>,
  );
}

describe("CerebroToolsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetAgentTools.mockResolvedValue([
      {
        name: "list_issues",
        description: "List workspace issues.",
        enabled: true,
        config: {},
      },
    ]);
    mockUseQuery.mockReturnValue({
      data: [
        {
          name: "list_issues",
          description: "List workspace issues.",
          enabled: true,
          config: {},
        },
      ],
      isLoading: false,
      isError: false,
    });
  });

  it("loads cloud-agent tools with workspace context and renders labels", async () => {
    renderToolsTab();

    expect(screen.getByText("list_issues")).toBeInTheDocument();
    expect(screen.getByText("List workspace issues.")).toBeInTheDocument();
    const options = mockUseQuery.mock.calls[0]?.[0] as { queryFn: () => Promise<unknown> };
    await options.queryFn();
    expect(mockGetAgentTools).toHaveBeenCalledWith("agent-1", { workspace_id: "ws-1" });
  });

  it("shows the local-runtime explanation instead of the empty registry state", () => {
    renderToolsTab({ ...baseAgent, runtime_mode: "local" });

    expect(screen.getByText("Gateway tools are cloud-only")).toBeInTheDocument();
    expect(
      screen.getByText(/These toggles only apply to cloud-runtime agents/i),
    ).toBeInTheDocument();
    expect(screen.queryByText("No tools available")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/No tools are registered in the gateway runtime/i),
    ).not.toBeInTheDocument();
    expect(mockUseQuery).toHaveBeenCalledWith(expect.objectContaining({ enabled: false }));
    expect(mockGetAgentTools).not.toHaveBeenCalled();
  });
});
