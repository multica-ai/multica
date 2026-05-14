// @vitest-environment jsdom

// CEREBRO-PATCH(agent-tools-tab-local-empty): regression coverage for local agents with no gateway tools.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockGetAgentTools = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/api", () => ({
  api: {
    getAgentTools: (...args: unknown[]) => mockGetAgentTools(...args),
    updateAgentTool: vi.fn(),
  },
}));

vi.mock("sonner", () => ({
  toast: {
    error: vi.fn(),
    success: vi.fn(),
  },
}));

import { CerebroToolsTab } from "./cerebro-tools-tab";

const localAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Lando",
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
  persona_sandbox: "",
};

function renderToolsTab(agent: Agent) {
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
        <CerebroToolsTab agent={agent} />
      </QueryClientProvider>
    </I18nProvider>,
  );
}

describe("CerebroToolsTab", () => {
  beforeEach(() => {
    vi.clearAllMocks();
    mockGetAgentTools.mockResolvedValue([]);
  });

  it("explains that gateway tools only apply to cloud-runtime agents when a local agent has no tools", async () => {
    renderToolsTab(localAgent);

    expect(await screen.findByText("Gateway tools are cloud-only")).toBeInTheDocument();
    expect(
      screen.getByText(/These toggles only apply to cloud-runtime agents/i),
    ).toBeInTheDocument();
    expect(screen.queryByText("No tools available")).not.toBeInTheDocument();
    expect(
      screen.queryByText(/No tools are registered in the gateway runtime/i),
    ).not.toBeInTheDocument();
  });
});
