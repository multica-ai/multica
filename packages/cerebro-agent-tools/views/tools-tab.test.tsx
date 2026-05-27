// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockListRuntimes = vi.hoisted(() => vi.fn());
const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockUseFeatureFlag = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => mockUseFeatureFlag(key),
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
      listRuntimeTools: mockListRuntimeTools,
      cerebroRequest: mockCerebroRequest,
    },
  };
});

import { CerebroToolsTab } from "./tools-tab";

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

function renderToolsTab(agent: Agent = baseAgent, canEdit = true) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroToolsTab agent={agent} canEdit={canEdit} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  mockListRuntimes.mockResolvedValue([
    { id: "runtime-1", name: "sara-mac-mini", workspace_id: "ws-1" },
  ]);
  mockListRuntimeTools.mockResolvedValue([]);
  mockCerebroRequest.mockResolvedValue([]);
  // Default: unified tool-policy flag off → keep the legacy override card.
  mockUseFeatureFlag.mockReturnValue(false);
});

describe("CerebroToolsTab", () => {
  it("renders the AgentToolsCard for local agents", async () => {
    renderToolsTab({ ...baseAgent, runtime_mode: "local" });

    expect(await screen.findByText(/Tools på agenten/i)).toBeInTheDocument();
  });

  it("renders the AgentToolsCard for cloud agents", async () => {
    renderToolsTab();

    expect(await screen.findByText(/Tools på agenten/i)).toBeInTheDocument();
    expect(
      await screen.findByText(/daemon scanner ved næste heartbeat/i),
    ).toBeInTheDocument();
  });

  it("hides override controls when canEdit is false", async () => {
    mockListRuntimeTools.mockResolvedValue([
      {
        name: "read_issue",
        description: "Read an issue",
        source: "cloud",
        mcp_server_name: "",
        enabled: true,
      },
    ]);
    renderToolsTab(baseAgent, false);

    expect(await screen.findByText("read_issue")).toBeInTheDocument();
    expect(
      screen.queryByRole("combobox", { name: /Skift override/i }),
    ).not.toBeInTheDocument();
  });

  it("renders the unified ToolPolicyTable when the flag is on", async () => {
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    renderToolsTab();

    // The FIR-2230 table replaces the legacy override card on the agent page.
    expect(await screen.findByTestId("tool-policy-table")).toBeInTheDocument();
    expect(screen.queryByText(/Tools på agenten/i)).not.toBeInTheDocument();
  });

  it("renders the simple table when only the simple flag is on", async () => {
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_simple_tool_policy",
    );
    renderToolsTab();

    expect(
      await screen.findByTestId("simple-tool-policy-table"),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Tools på agenten/i)).not.toBeInTheDocument();
    expect(screen.queryByTestId("tool-policy-table")).not.toBeInTheDocument();
  });

  it("the rich power-view wins when both tool-policy flags are on", async () => {
    mockUseFeatureFlag.mockImplementation(
      (key: string) =>
        key === "cerebro_tool_policy" || key === "cerebro_simple_tool_policy",
    );
    renderToolsTab();

    expect(await screen.findByTestId("tool-policy-table")).toBeInTheDocument();
    expect(
      screen.queryByTestId("simple-tool-policy-table"),
    ).not.toBeInTheDocument();
  });

  it("binds the agent's owner as the user ceiling on the agent page", async () => {
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    renderToolsTab();

    // The Effective column must reflect the full Runtime › Agent › Group › User
    // chain, so the table fetch carries the agent's runtime, the agent itself,
    // AND the owner's user id (the groups are expanded server-side from it).
    await waitFor(() => expect(mockCerebroRequest).toHaveBeenCalled());
    const urls = mockCerebroRequest.mock.calls.map(([url]) => String(url));
    const tableUrl = urls.find((u) => u.includes("/tool-policy?"));
    expect(tableUrl).toBeDefined();
    expect(tableUrl).toContain("agent_id=agent-1");
    expect(tableUrl).toContain("runtime_id=runtime-1");
    expect(tableUrl).toContain("user_id=user-1");
  });
});
