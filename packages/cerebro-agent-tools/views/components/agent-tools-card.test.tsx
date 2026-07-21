import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";
import type { RuntimeTool, RuntimeToolEffectiveAccess } from "@multica/cerebro-types";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListRuntimeToolEffectiveAccess = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>("@multica/core/api");
  return { ...actual, api: { ...actual.api, listRuntimeTools: mockListRuntimeTools, listRuntimeToolEffectiveAccess: mockListRuntimeToolEffectiveAccess } };
});

vi.mock("@multica/cerebro-tool-policy/views", () => ({
  FirtalRegistryRowConfigure: () => null,
}));

import { AgentToolsCard } from "./agent-tools-card";

const agent = {
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
} as Agent;

const tool: RuntimeTool = {
  id: "tool-1",
  runtime_id: "rt-1",
  name: "firtal_bq_query",
  source: "cloud",
  mcp_server_name: "",
  description: "BigQuery read-only",
  enabled: true,
  last_scanned_at: null,
};

const effective: RuntimeToolEffectiveAccess = {
  descriptor: { tool_key: tool.name, display_name: tool.name, description: tool.description, source: "platform", risk_class: "read", protocols: ["native_tool_loop"], recommended_default_policy: "allow" },
  inventory: { runtime_id: "rt-1", tool_name: tool.name, source: "cloud", mcp_server_name: "", enabled: true },
  policy: { effective: "deny", reason: "Denied by agent policy" },
  protocol: { effective: "allow", required_protocols: [], runtime_protocols: [], selected_protocol: "", supports_ask: true, unsupported_message: "" },
  credential: { effective: "allow", reason: "No credential required" },
  exposure_effective: { effective: false, reason: "Denied by agent policy" },
};

function renderCard(value: Agent = agent) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}><AgentToolsCard agent={value} canEdit runtimeName="Gateway" /></QueryClientProvider>);
}

describe("AgentToolsCard", () => {
  beforeEach(() => {
    mockListRuntimeTools.mockReset().mockResolvedValue([tool]);
    mockListRuntimeToolEffectiveAccess.mockReset().mockResolvedValue([effective]);
  });

  it("renders the canonical effective policy decision without a legacy override editor", async () => {
    renderCard();
    expect(await screen.findByText("firtal_bq_query")).toBeInTheDocument();
    expect(screen.getByText("Inactive")).toBeInTheDocument();
    expect(screen.getByText("Denied by agent policy")).toBeInTheDocument();
    expect(screen.getByText(/Settings → Permissions/)).toBeInTheDocument();
    expect(screen.queryByText("Force on")).not.toBeInTheDocument();
    expect(screen.queryByText("Force off")).not.toBeInTheDocument();
  });

  it("does not query tool inventory when the agent has no runtime", () => {
    renderCard({ ...agent, runtime_id: "" });
    expect(screen.getByText("The agent has no runtime assigned yet.")).toBeInTheDocument();
    expect(mockListRuntimeTools).not.toHaveBeenCalled();
  });
});
