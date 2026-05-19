// @vitest-environment jsdom
// CEREBRO-PATCH(agent-tools-tab-test): JEH-1710 bid 4 — wrapper test covers
// the local-agent branch and the cloud-agent branch's admin-gate handoff to
// AgentToolsCard. The card itself owns the override-table assertions; this
// test is intentionally thin.

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import { I18nProvider } from "@multica/core/i18n/react";
import enCommon from "../../../locales/en/common.json";
import enAgents from "../../../locales/en/agents.json";

const TEST_RESOURCES = { en: { common: enCommon, agents: enAgents } };

const mockUseAuthStore = vi.hoisted(() => vi.fn());
const mockListRuntimes = vi.hoisted(() => vi.fn());
const mockListMembers = vi.hoisted(() => vi.fn());
const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListAgentToolOverrides = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/core/auth", () => ({
  useAuthStore: (selector: (s: unknown) => unknown) =>
    mockUseAuthStore(selector),
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuntimes: mockListRuntimes,
      listMembers: mockListMembers,
      listRuntimeTools: mockListRuntimeTools,
      listAgentToolOverrides: mockListAgentToolOverrides,
    },
  };
});

import { CerebroToolsTab } from "./cerebro-tools-tab";

const baseAgent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "runtime-1",
  name: "Tine",
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
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <I18nProvider locale="en" resources={TEST_RESOURCES}>
        <CerebroToolsTab agent={agent} />
      </I18nProvider>
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockUseAuthStore.mockReturnValue({ id: "user-1", email: "x@example.com" });
  mockListMembers.mockResolvedValue([
    { user_id: "user-1", role: "admin", name: "Owner", email: "x@example.com" },
  ]);
  mockListRuntimes.mockResolvedValue([
    { id: "runtime-1", name: "sara-mac-mini", workspace_id: "ws-1" },
  ]);
  mockListRuntimeTools.mockResolvedValue([]);
  mockListAgentToolOverrides.mockResolvedValue([]);
});

describe("CerebroToolsTab", () => {
  it("shows the local-runtime explanation for local agents and skips runtime/override fetches", () => {
    renderToolsTab({ ...baseAgent, runtime_mode: "local" });

    expect(screen.getByText("Gateway tools are cloud-only")).toBeInTheDocument();
    expect(
      screen.getByText(/These toggles only apply to cloud-runtime agents/i),
    ).toBeInTheDocument();
    expect(mockListRuntimes).not.toHaveBeenCalled();
    expect(mockListRuntimeTools).not.toHaveBeenCalled();
  });

  it("renders the AgentToolsCard for cloud agents (empty registry surfaces the heartbeat hint)", async () => {
    renderToolsTab();

    // The card title is rendered immediately; the empty-state hint follows
    // once the runtime-tools fetch resolves.
    expect(await screen.findByText(/Tools på agenten/i)).toBeInTheDocument();
    expect(
      await screen.findByText(/daemon scanner ved næste heartbeat/i),
    ).toBeInTheDocument();
  });
});
