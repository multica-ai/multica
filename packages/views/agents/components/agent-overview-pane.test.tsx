// @vitest-environment jsdom

import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent, AgentRuntime } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../locales/en/common.json";
import enAgents from "../../locales/en/agents.json";
import {
  NavigationProvider,
  type NavigationAdapter,
} from "../../navigation";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));
vi.mock("@multica/core/paths", () => ({
  useWorkspacePaths: () => ({ issues: () => "/acme/issues" }),
}));
vi.mock("@multica/core/workspace/queries", () => ({
  squadListOptions: () => ({
    queryKey: ["squads"],
    queryFn: () => Promise.resolve([]),
  }),
  workspaceKeys: { squads: () => ["squads"] },
}));
vi.mock("./agent-overview-summary", () => ({
  AgentOverviewSummary: () => <div>agent-overview-summary</div>,
}));
vi.mock("./agent-detail-inspector", () => ({
  AgentDetailInspector: () => <div>agent-detail-inspector</div>,
}));
vi.mock("./tabs/instructions-tab", () => ({
  InstructionsTab: () => <div>instructions-tab</div>,
}));
vi.mock("./tabs/skills-tab", () => ({
  SkillsTab: () => <div>skills-tab</div>,
}));

import { AgentOverviewPane } from "./agent-overview-pane";

const baseAgent: Agent = {
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
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
  archived_at: null,
  archived_by: null,
};

const runtime: AgentRuntime = {
  id: "runtime-1",
  workspace_id: "ws-1",
  daemon_id: null,
  name: "Runtime",
  runtime_mode: "local",
  provider: "codex",
  launch_header: "",
  status: "online",
  device_info: "",
  metadata: {},
  owner_id: null,
  visibility: "private",
  last_seen_at: null,
  created_at: "2026-05-28T00:00:00Z",
  updated_at: "2026-05-28T00:00:00Z",
};

function renderPane() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  const navigation: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/agents/agent-1",
    searchParams: new URLSearchParams(),
    getShareableUrl: (path) => path,
  };
  return render(
    <I18nProvider locale="en" resources={TEST_RESOURCES}>
      <NavigationProvider value={navigation}>
        <QueryClientProvider client={queryClient}>
          <AgentOverviewPane
            agent={baseAgent}
            runtime={runtime}
            owner={null}
            runtimes={[runtime]}
            members={[]}
            onUpdate={vi.fn().mockResolvedValue(undefined)}
            canEdit
          />
        </QueryClientProvider>
      </NavigationProvider>
    </I18nProvider>,
  );
}

describe("AgentOverviewPane role center", () => {
  it("keeps the approved role-center tabs and excludes advanced administration", () => {
    renderPane();

    for (const label of ["Overview", "Skills", "Instructions", "General"]) {
      expect(screen.getByRole("tab", { name: label })).toBeInTheDocument();
    }
    for (const label of ["Work", "Capabilities", "Settings", "MCP", "Environment"]) {
      expect(screen.queryByRole("tab", { name: label })).not.toBeInTheDocument();
    }
  });

  it("links the full work surface back to Tasks", () => {
    renderPane();

    expect(screen.getByRole("link", { name: /Tasks/i })).toHaveAttribute(
      "href",
      "/acme/issues",
    );
  });
});
