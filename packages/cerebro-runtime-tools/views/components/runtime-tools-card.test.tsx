import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types/agent";
import type { RuntimeTool } from "@multica/cerebro-types";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListRuntimeToolGrants = vi.hoisted(() => vi.fn());
const mockListCerebroGroups = vi.hoisted(() => vi.fn());
const mockListMembers = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuntimeTools: mockListRuntimeTools,
      listRuntimeToolGrants: mockListRuntimeToolGrants,
      listCerebroGroups: mockListCerebroGroups,
      listMembers: mockListMembers,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

import { RuntimeToolsCard } from "./runtime-tools-card";

const runtime: AgentRuntime = {
  id: "rt-1",
  workspace_id: "ws-1",
  daemon_id: "daemon-1",
  name: "sara-mac-mini",
  runtime_mode: "local",
  provider: "claude",
  launch_header: "",
  status: "online",
  device_info: "host.local",
  metadata: {},
  owner_id: null,
  sandbox_enabled: null,
  persona_sandbox: "",
  visibility: "private",
  timezone: "UTC",
  capabilities: {},
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

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
  mockListRuntimeToolGrants.mockReset();
  mockListCerebroGroups.mockReset();
  mockListMembers.mockReset();
  mockListRuntimeToolGrants.mockResolvedValue({
    group_grants: [],
    user_grants: [],
  });
  mockListCerebroGroups.mockResolvedValue([]);
  mockListMembers.mockResolvedValue([]);
});

describe("RuntimeToolsCard", () => {
  it("shows the empty state when the registry has no tools yet", async () => {
    mockListRuntimeTools.mockResolvedValue([]);

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    await waitFor(() =>
      expect(
        screen.getByText(/daemon scanner ved næste heartbeat/i),
      ).toBeInTheDocument(),
    );
  });

  it("renders one row per tool with source badge + explicit empty access labels", async () => {
    const tools: RuntimeTool[] = [
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
        description: "",
        enabled: false,
        last_scanned_at: null,
      },
    ];
    mockListRuntimeTools.mockResolvedValue(tools);

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    await waitFor(() =>
      expect(screen.getByText("firtal_bq_query")).toBeInTheDocument(),
    );
    expect(screen.getByText("github_create_issue")).toBeInTheDocument();
    // The mockup spec calls for explicit "ingen specifikke" rather than blanks.
    expect(screen.getAllByText(/ingen specifikke/i).length).toBeGreaterThan(0);
    // MCP rows show the server name in the badge.
    expect(screen.getByText(/github-server/)).toBeInTheDocument();
  });

  it("falls back to an error state when the tools fetch fails", async () => {
    mockListRuntimeTools.mockRejectedValue(new Error("boom"));

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    await waitFor(() =>
      expect(
        screen.getByText(/kunne ikke hente tools/i),
      ).toBeInTheDocument(),
    );
  });
});
