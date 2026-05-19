import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type {
  AgentToolOverride,
  RuntimeTool,
} from "@multica/cerebro-types";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListAgentToolOverrides = vi.hoisted(() => vi.fn());
const mockSetAgentToolOverride = vi.hoisted(() => vi.fn());
const mockClearAgentToolOverride = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuntimeTools: mockListRuntimeTools,
      listAgentToolOverrides: mockListAgentToolOverrides,
      setAgentToolOverride: mockSetAgentToolOverride,
      clearAgentToolOverride: mockClearAgentToolOverride,
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

function renderWithClient(node: React.ReactNode) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>{node}</QueryClientProvider>,
  );
}

beforeEach(() => {
  mockListRuntimeTools.mockReset();
  mockListAgentToolOverrides.mockReset();
  mockSetAgentToolOverride.mockReset();
  mockClearAgentToolOverride.mockReset();
  mockSetAgentToolOverride.mockResolvedValue(undefined);
  mockClearAgentToolOverride.mockResolvedValue(undefined);
});

describe("AgentToolsCard", () => {
  it("renders one row per runtime tool, marks override rows, and computes effective state", async () => {
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue(overrides);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );

    // Inherit row: shows "Arver" tag.
    expect(screen.getAllByText("— Arver").length).toBeGreaterThan(0);
    // Force-off row: shows "Tving fra" tag.
    expect(screen.getByText("Tving fra")).toBeInTheDocument();
    // Force-on row: shows "Tving på" tag.
    expect(screen.getByText("Tving på")).toBeInTheDocument();

    // Effective: firtal_bq_query (on, inherit) = Aktiv;
    // github_create_issue (force_off) = Inaktiv;
    // slack_post_message (force_on) = Aktiv.
    const aktive = screen.getAllByText("Aktiv");
    expect(aktive.length).toBe(2);
    const inaktive = screen.getAllByText("Inaktiv");
    expect(inaktive.length).toBe(1);
  });

  it("filters to override-only rows", async () => {
    const user = userEvent.setup();
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue(overrides);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );

    await user.click(screen.getByRole("radio", { name: /Kun override/i }));

    // Inherit row is hidden.
    expect(screen.queryByText("firtal_bq_query")).not.toBeInTheDocument();
    // Override rows are still there.
    expect(screen.getByText("github_create_issue")).toBeInTheDocument();
    expect(screen.getByText("slack_post_message")).toBeInTheDocument();
  });

  it("filters to effectively active rows", async () => {
    const user = userEvent.setup();
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue(overrides);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );

    await user.click(screen.getByRole("radio", { name: /Effektivt aktive/i }));

    // The two effectively-on rows survive.
    expect(screen.getByText("firtal_bq_query")).toBeInTheDocument();
    expect(screen.getByText("slack_post_message")).toBeInTheDocument();
    // The forced-off row is hidden.
    expect(screen.queryByText("github_create_issue")).not.toBeInTheDocument();
  });

  it("hides row actions when canEdit is false", async () => {
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue(overrides);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={false} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );

    // No action triggers for non-admins.
    expect(
      screen.queryByRole("combobox", { name: /Skift override/i }),
    ).not.toBeInTheDocument();
  });

  it("calls setAgentToolOverride when the admin picks Tving på", async () => {
    const user = userEvent.setup();
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue([]);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );

    const selects = screen.getAllByRole("combobox", { name: /Skift override/i });
    expect(selects.length).toBe(3);

    // Pick "Tving fra" on the first row (firtal_bq_query, runtime-on).
    await user.selectOptions(selects[0]!, "force_off");

    expect(mockSetAgentToolOverride).toHaveBeenCalledWith(
      "agent-1",
      "firtal_bq_query",
      false,
    );
  });

  it("calls clearAgentToolOverride when the admin picks Arv runtime on an override row", async () => {
    const user = userEvent.setup();
    mockListRuntimeTools.mockResolvedValue(runtimeTools);
    mockListAgentToolOverrides.mockResolvedValue(overrides);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(screen.getByText("github_create_issue")).toBeInTheDocument(),
    );

    // Find the select scoped to the github_create_issue row (the force_off row).
    const githubRow = screen
      .getByText("github_create_issue")
      .closest("tr") as HTMLTableRowElement;
    const selectInRow = githubRow.querySelector(
      "select[aria-label='Skift override']",
    ) as HTMLSelectElement;
    expect(selectInRow.value).toBe("force_off");

    await user.selectOptions(selectInRow, "inherit");

    expect(mockClearAgentToolOverride).toHaveBeenCalledWith(
      "agent-1",
      "github_create_issue",
    );
  });

  it("falls back to an explicit empty banner when the runtime has no tools yet", async () => {
    mockListRuntimeTools.mockResolvedValue([]);
    mockListAgentToolOverrides.mockResolvedValue([]);

    renderWithClient(
      <AgentToolsCard agent={agent} canEdit={true} runtimeName="sara-mac-mini" />,
    );

    await waitFor(() =>
      expect(
        screen.getByText(/daemon scanner ved næste heartbeat/i),
      ).toBeInTheDocument(),
    );
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
