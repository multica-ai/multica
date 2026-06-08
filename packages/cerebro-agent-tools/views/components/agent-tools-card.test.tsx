import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type {
  AgentToolOverride,
  RuntimeTool,
  RuntimeToolEffectiveAccess,
} from "@multica/cerebro-types";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListRuntimeToolEffectiveAccess = vi.hoisted(() => vi.fn());
const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockUseFeatureFlag = vi.hoisted(() => vi.fn(() => false));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: mockUseFeatureFlag,
}));

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuntimeTools: mockListRuntimeTools,
      listRuntimeToolEffectiveAccess: mockListRuntimeToolEffectiveAccess,
      cerebroRequest: mockCerebroRequest,
    },
  };
});

import { AgentToolsCard } from "./agent-tools-card";

const agent: Agent = {
  id: "agent-1",
  workspace_id: "ws-1",
  runtime_id: "rt-1",
  name: "Tine",
  description: "",
  instructions: "",
  avatar_url: null,
  runtime_mode: "cloud",
  runtime_config: {},
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

const runtimeTools: RuntimeTool[] = [
  {
    id: "t1",
    runtime_id: "rt-1",
    name: "firtal_bq_query",
    source: "cloud",
    mcp_server_name: "",
    description: "BigQuery read-only",
    enabled: true,
    last_scanned_at: null,
  },
  {
    id: "t2",
    runtime_id: "rt-1",
    name: "github_create_issue",
    source: "mcp",
    mcp_server_name: "github-server",
    description: "Open issues on GitHub",
    enabled: true,
    last_scanned_at: null,
  },
  {
    id: "t3",
    runtime_id: "rt-1",
    name: "slack_post_message",
    source: "mcp",
    mcp_server_name: "slack-server",
    description: "Post a Slack message",
    enabled: false,
    last_scanned_at: null,
  },
];

const overrides: AgentToolOverride[] = [
  // Tine has github_create_issue forced OFF
  {
    agent_id: "agent-1",
    tool_name: "github_create_issue",
    enabled: false,
    updated_at: "2026-05-19T00:00:00Z",
  },
  // and slack_post_message forced ON (runtime default is off)
  {
    agent_id: "agent-1",
    tool_name: "slack_post_message",
    enabled: true,
    updated_at: "2026-05-19T00:00:00Z",
  },
];

function effectiveAccess(
  tool: RuntimeTool,
  effective: boolean,
  reason = effective ? "allowed by policy, runtime grants, protocol, and credential checks" : "blocked by policy",
): RuntimeToolEffectiveAccess {
  return {
    descriptor: {
      tool_key: tool.name,
      display_name: tool.name,
      description: tool.description,
      source: tool.source === "mcp" ? "mcp" : "platform",
      risk_class: "read",
      protocols: tool.source === "mcp" ? ["mcp_stdio"] : ["native_tool_loop"],
      recommended_default_policy: "allow",
    },
    inventory: {
      runtime_id: tool.runtime_id,
      tool_name: tool.name,
      source: tool.source,
      mcp_server_name: tool.mcp_server_name,
      enabled: tool.enabled,
    },
    policy: { effective: "allow", reason: "Allowed by default" },
    runtime_grant: { effective: effective ? "allow" : "deny", reason },
    protocol: {
      effective: "supported",
      required_protocols: [],
      runtime_protocols: ["native_tool_loop", "mcp_stdio"],
      selected_protocol: tool.source === "mcp" ? "mcp_stdio" : "native_tool_loop",
      supports_ask: false,
    },
    credential: { effective: "not_required", reason: "No credential required" },
    exposure_effective: { effective, reason },
    layers: {},
  };
}

const effectiveRows = [
  effectiveAccess(runtimeTools[0]!, true),
  effectiveAccess(runtimeTools[1]!, false, "Agent override lukker tool'et"),
  effectiveAccess(runtimeTools[2]!, true, "Agent override åbner tool'et"),
];

function renderWithClient(
  node: React.ReactNode,
  data: {
    tools?: RuntimeTool[];
    overrides?: AgentToolOverride[];
    effective?: RuntimeToolEffectiveAccess[];
  } = {},
) {
  const qc = new QueryClient({
    defaultOptions: {
      queries: { retry: false, staleTime: Infinity, gcTime: Infinity },
    },
  });
  if (data.tools) {
    qc.setQueryData(["cerebro", "runtime-tools", "rt-1"], data.tools);
  }
  if (data.overrides) {
    qc.setQueryData(
      ["cerebro", "agent-tool-overrides", "agent-1"],
      data.overrides,
    );
  }
  qc.setQueryData(
    ["cerebro", "runtime-tool-effective-access", "rt-1", "agent", "agent-1"],
    data.effective ?? effectiveRows,
  );
  return render(
    <QueryClientProvider client={qc}>{node}</QueryClientProvider>,
  );
}

beforeEach(() => {
  mockListRuntimeTools.mockReset();
  mockListRuntimeToolEffectiveAccess.mockReset();
  mockListRuntimeToolEffectiveAccess.mockResolvedValue(effectiveRows);
  mockCerebroRequest.mockReset();
  mockCerebroRequest.mockResolvedValue(overrides);
  mockUseFeatureFlag.mockReturnValue(false);
});

describe("AgentToolsCard", () => {
  it("renders one row per runtime tool, marks override rows, and computes effective state", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides },
    );

    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();

    // Inherit row: shows "Arver" tag.
    expect(screen.getAllByText("— Inherits").length).toBeGreaterThan(0);
    // The override badges are <span>s. The same words appear inside <option>
    // elements of the row override-picker, so scope to span to pick the badge.
    expect(
      screen.getByText("Tving fra", { selector: "span" }),
    ).toBeInTheDocument();
    expect(
      screen.getByText("Tving på", { selector: "span" }),
    ).toBeInTheDocument();

    // Effective: firtal_bq_query (on, inherit) = Aktiv;
    // github_create_issue (force_off) = Inaktiv;
    // slack_post_message (force_on) = Aktiv.
    const aktive = screen.getAllByText("Aktiv");
    expect(aktive.length).toBe(2);
    const inaktive = screen.getAllByText("Inaktiv");
    expect(inaktive.length).toBe(1);
  });

  it("filters to override-only rows", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides },
    );

    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();

    fireEvent.click(screen.getByText("Kun override (2)"));

    // Inherit row is hidden.
    expect(screen.queryByText("firtal_bq_query")).not.toBeInTheDocument();
    // Override rows are still there.
    expect(screen.getByText("github_create_issue")).toBeInTheDocument();
    expect(screen.getByText("slack_post_message")).toBeInTheDocument();
  });

  it("filters to effectively active rows", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides },
    );

    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();

    fireEvent.click(screen.getByText("Effektivt aktive (2)"));

    // The two effectively-on rows survive.
    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();
    expect(screen.getByText("slack_post_message")).toBeInTheDocument();
    // The forced-off row is hidden.
    expect(screen.queryByText("github_create_issue")).not.toBeInTheDocument();
  });

  it("lets server-computed deny win over a force-on agent override", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      {
        tools: runtimeTools,
        overrides,
        effective: [
          effectiveRows[0]!,
          effectiveRows[1]!,
          effectiveAccess(runtimeTools[2]!, false, "Denied by workspace"),
        ],
      },
    );

    const slackRow = screen
      .getByText("slack_post_message")
      .closest("tr") as HTMLTableRowElement;
    expect(slackRow).toHaveTextContent("Tving på");
    expect(slackRow).toHaveTextContent("Inaktiv");
    expect(slackRow).toHaveTextContent("Denied by workspace");
  });

  it("hides row actions when canEdit is false", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={false} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides },
    );

    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();

    // No action triggers for non-admins.
    expect(
      screen.queryByRole("combobox", { name: /Skift override/i }),
    ).not.toBeInTheDocument();
  });

  it("calls setAgentToolOverride when the admin picks Tving på", async () => {
    const user = userEvent.setup();
    mockCerebroRequest.mockResolvedValue([]);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides: [] },
    );

    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();

    const selects = screen.getAllByRole("combobox", { name: /Skift override/i });
    expect(selects.length).toBe(3);

    // Pick "Tving fra" on the first row (firtal_bq_query, runtime-on).
    await user.selectOptions(selects[0]!, "force_off");

    expect(mockCerebroRequest).toHaveBeenCalledWith(
      "/api/agents/agent-1/tool-overrides/firtal_bq_query",
      { method: "PUT", body: JSON.stringify({ enabled: false }) },
    );
  });

  it("calls clearAgentToolOverride when the admin picks Arv runtime on an override row", async () => {
    const user = userEvent.setup();

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: runtimeTools, overrides },
    );

    expect(screen.getByText("github_create_issue")).toBeInTheDocument();

    // Find the select scoped to the github_create_issue row (the force_off row).
    const githubRow = screen
      .getByText("github_create_issue")
      .closest("tr") as HTMLTableRowElement;
    const selectInRow = githubRow.querySelector(
      "select[aria-label='Skift override']",
    ) as HTMLSelectElement;
    expect(selectInRow.value).toBe("force_off");

    await user.selectOptions(selectInRow, "inherit");

    expect(mockCerebroRequest).toHaveBeenCalledWith(
      "/api/agents/agent-1/tool-overrides/github_create_issue",
      { method: "DELETE" },
    );
  });

  it("falls back to an explicit empty banner when the runtime has no tools yet", () => {
    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
      { tools: [], overrides: [] },
    );

    expect(
      screen.getByText(/daemon scanner ved næste heartbeat/i),
    ).toBeInTheDocument();
  });

  it("renders a helper line when the agent has no runtime attached yet", () => {
    renderWithClient(
      <AgentToolsCard
        agent={{ ...agent, runtime_id: "" }}
        canEdit={true}
      />,
    );

    expect(
      screen.getByText(/Agenten har ikke en runtime tilknyttet endnu/i),
    ).toBeInTheDocument();
  });
});
