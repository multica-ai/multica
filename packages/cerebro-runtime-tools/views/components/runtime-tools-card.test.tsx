import type React from "react";
import { describe, it, expect, vi, beforeEach } from "vitest";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { AgentRuntime } from "@multica/core/types/agent";

const mockListRuntimeTools = vi.hoisted(() => vi.fn());
const mockCerebroRequest = vi.hoisted(() => vi.fn());
const mockGetRuntimeAccessDiagnostics = vi.hoisted(() => vi.fn());

vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return {
    ...actual,
    api: {
      ...actual.api,
      listRuntimeTools: mockListRuntimeTools,
      cerebroRequest: mockCerebroRequest,
      getRuntimeAccessDiagnostics: mockGetRuntimeAccessDiagnostics,
    },
  };
});

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/cerebro-feature-flags", () => ({ useFeatureFlag: () => false }));

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
  visibility: "private",
  timezone: "UTC",
  capabilities: {},
  last_seen_at: null,
  created_at: "2026-01-01T00:00:00Z",
  updated_at: "2026-01-01T00:00:00Z",
};

function renderWithClient(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(<QueryClientProvider client={client}>{node}</QueryClientProvider>);
}

beforeEach(() => {
  mockListRuntimeTools.mockReset();
  mockCerebroRequest.mockReset();
  mockGetRuntimeAccessDiagnostics.mockReset();
  mockListRuntimeTools.mockResolvedValue([]);
  mockCerebroRequest.mockResolvedValue({ tools: [] });
  mockGetRuntimeAccessDiagnostics.mockResolvedValue({
    runtime_id: "rt-1",
    provider: "claude",
    status: "online",
    diagnostics: [
      {
        code: "provider_probe",
        state: "success",
        title: "Provider capability probe",
        message: "The provider probe measured 12 callable capabilities.",
        affected_capability: "provider:claude",
        source_policy: "Provider probe",
        recovery_action: "Run Scan now after changing the provider.",
        version: "sha256:provider",
      },
      {
        code: "mcp_discovery",
        state: "stale",
        title: "MCP discovery",
        message: "The latest MCP discovery is older than 30m0s.",
        affected_capability: "mcp:*",
        source_policy: "MCP tools/list",
        recovery_action: "Run Scan now.",
        version: "sha256:mcp",
      },
    ],
  });
});

describe("RuntimeToolsCard", () => {
  it("always renders canonical tool policy even when the feature flag is off", async () => {
    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    expect(await screen.findByTestId("tool-policy-table")).toBeInTheDocument();
    expect(screen.queryByText(/none specific/i)).not.toBeInTheDocument();
  });

  it("keeps the live scan-now inventory action", async () => {
    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    fireEvent.click(await screen.findByRole("button", { name: /scan now/i }));
    await waitFor(() =>
      expect(mockCerebroRequest).toHaveBeenCalledWith(
        "/api/runtimes/rt-1/tools/scan-now",
        { method: "POST" },
      ),
    );
  });

  it("shows provider probe and MCP discovery as separate diagnostics", async () => {
    renderWithClient(
      <RuntimeToolsCard runtime={runtime} workspaceId="ws-1" canEdit={true} />,
    );

    expect(await screen.findByText("Provider capability probe")).toBeInTheDocument();
    expect(screen.getByText("MCP discovery")).toBeInTheDocument();
    expect(screen.getByText("sha256:provider")).toBeInTheDocument();
    expect(screen.getByText("sha256:mcp")).toBeInTheDocument();
  });
});
