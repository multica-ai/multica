// @vitest-environment jsdom

import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { Agent } from "@multica/core/types";

const mockCerebroRequest = vi.hoisted(() => vi.fn());

// Keep the real parseWithFallback so the malformed-response path is exercised
// end-to-end (API Response Compatibility rule); only the network call is mocked.
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: { ...actual.api, cerebroRequest: mockCerebroRequest },
  };
});

import { CerebroCapabilitiesTab } from "./capabilities-tab";

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

function renderTab(agent: Agent = baseAgent) {
  const qc = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={qc}>
      <CerebroCapabilitiesTab agent={agent} />
    </QueryClientProvider>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
});

describe("CerebroCapabilitiesTab", () => {
  it("renders the four capability sections from a well-formed response", async () => {
    mockCerebroRequest.mockResolvedValue({
      agent_id: "agent-1",
      name: "Tine",
      model: "claude",
      description: "",
      skills: [{ id: "s1", name: "deploy", description: "Ship a PR" }],
      tools: [
        { name: "read_issue", enabled: true },
        { name: "delete_issue", enabled: false },
      ],
      credentials: [{ name: "GITHUB_TOKEN", type: "secret", description: "" }],
      limits: {
        sandbox: { network_allowlist: ["api.github.com:443"] },
        mcp_servers: ["multica"],
        has_mcp_config: true,
      },
    });

    renderTab();

    expect(await screen.findByText("Can do")).toBeInTheDocument();
    expect(screen.getByText("May use")).toBeInTheDocument();
    expect(screen.getByText("Has access to")).toBeInTheDocument();
    expect(screen.getByText("Limited by")).toBeInTheDocument();

    expect(screen.getByText("deploy")).toBeInTheDocument();
    // Only enabled tools render; the disabled one is filtered out.
    expect(screen.getByText("read_issue")).toBeInTheDocument();
    expect(screen.queryByText("delete_issue")).not.toBeInTheDocument();
    expect(screen.getByText("GITHUB_TOKEN")).toBeInTheDocument();
    expect(screen.getByText("multica")).toBeInTheDocument();
  });

  it("survives a malformed response without throwing (fallback to empty)", async () => {
    // Missing fields, wrong types, null arrays — every defense at once.
    mockCerebroRequest.mockResolvedValue({
      agent_id: 123,
      skills: null,
      tools: "not-an-array",
      credentials: undefined,
      limits: { mcp_servers: null },
    });

    renderTab();

    // The card still renders its section scaffold and shows empty states
    // rather than white-screening.
    expect(await screen.findByText("Can do")).toBeInTheDocument();
    expect(screen.getByText("No skills loaded.")).toBeInTheDocument();
    expect(screen.getByText("No tools enabled.")).toBeInTheDocument();
    expect(screen.getByText("No credentials bound.")).toBeInTheDocument();
  });
});
