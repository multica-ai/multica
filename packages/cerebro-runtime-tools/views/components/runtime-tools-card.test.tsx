import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types/agent";
import type { RuntimeTool } from "@multica/cerebro-types";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockListRuntimeToolGrants = vi.hoisted(() => vi.fn());
const mockListCerebroGroups = vi.hoisted(() => vi.fn());
const mockListMembers = vi.hoisted(() => vi.fn());
const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockUseFeatureFlag = vi.hoisted(() => vi.fn());

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
      cerebroRequest: mockCerebroRequest,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({
  useWorkspaceId: () => "ws-1",
}));

vi.mock("@multica/cerebro-feature-flags", () => ({
  useFeatureFlag: (key: string) => mockUseFeatureFlag(key),
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
  mockCerebroRequest.mockResolvedValue({ tools: [] });
  // Default: unified tool-policy flag off → keep the legacy tools+grants card.
  mockUseFeatureFlag.mockReturnValue(false);
});

describe("RuntimeToolsCard", () => {
  it("shows the empty state when the registry has no tools yet", async () => {
    mockListRuntimeTools.mockResolvedValue([]);

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    await waitFor(() =>
      expect(
        screen.getByText(/daemon scans on next heartbeat/i),
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
    // The mockup spec calls for explicit "none specific" rather than blanks.
    expect(screen.getAllByText(/none specific/i).length).toBeGreaterThan(0);
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
        screen.getByText(/couldn't load tools/i),
      ).toBeInTheDocument(),
    );
  });

  it("renders the unified ToolPolicyTable when the flag is on", async () => {
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    mockCerebroRequest.mockResolvedValue({
      tools: [
        {
          tool_key: "bigquery.query",
          title: "BigQuery query",
          category: "data",
          source: "mcp",
          layers: { runtime: "allow", agent: null, group: null, user: null },
          effective: {
            setting: "allow",
            decided_by: "runtime",
            capped_by: "",
            reason: "",
          },
        },
      ],
    });

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    // The FIR-2230 table replaces the legacy enable+grants body on the runtime page.
    expect(await screen.findByTestId("tool-policy-table")).toBeInTheDocument();
    expect(await screen.findByText("BigQuery query")).toBeInTheDocument();
  });

  it("Scan now triggers a live daemon scan via the scan-now endpoint", async () => {
    const user = userEvent.setup();
    mockUseFeatureFlag.mockImplementation(
      (key: string) => key === "cerebro_tool_policy",
    );
    mockCerebroRequest.mockResolvedValue({ tools: [] });

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    const scanBtn = await screen.findByRole("button", { name: /scan now/i });
    await user.click(scanBtn);

    await waitFor(() => {
      const post = mockCerebroRequest.mock.calls.find(
        (c) =>
          typeof c[0] === "string" &&
          c[0].endsWith("/tools/scan-now") &&
          (c[1] as RequestInit | undefined)?.method === "POST",
      );
      expect(post).toBeTruthy();
      expect(post![0]).toBe("/api/runtimes/rt-1/tools/scan-now");
    });
  });

  it("shows the real Scan now button (not the old cache-only Refresh) even with the unified flag off", async () => {
    // FIR-2230 phase 7: the cache-only "Refresh" button is gone. The honest
    // "Scan now" is the single scan affordance and shows in the legacy card too.
    mockUseFeatureFlag.mockReturnValue(false);
    mockListRuntimeTools.mockResolvedValue([]);

    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    expect(
      await screen.findByRole("button", { name: /scan now/i }),
    ).toBeInTheDocument();
    expect(
      screen.queryByRole("button", { name: /^refresh$/i }),
    ).not.toBeInTheDocument();
  });
});
