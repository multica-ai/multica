import { screen } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";
import { renderWithI18n } from "../../test/i18n";
import { OperationsPage } from "./operations-page";

vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));
vi.mock("@multica/core/api", () => ({
  api: {
    getOperationalSummary: vi.fn().mockResolvedValue({
      workspace_id: "22222222-2222-4222-8222-222222222222",
      days: 7,
      timezone: "UTC",
      generated_at: "2026-08-29T12:00:00Z",
      pending: 3,
      approved: 8,
      denied: 2,
      expired: 1,
      failed: 4,
      intercepted_invocation_count: 14,
      declaration_only_count: 5,
      median_decision_time_ms: 1200,
      configured_agent_capability_gaps: 2,
    }),
    listOperationalCapabilities: vi.fn().mockResolvedValue({
      capabilities: [{
        name: "tool_invocation",
        transport_kind: "managed_mcp",
        provider_family: "anthropic",
        supported: false,
        offline_reason: "provider adapter unavailable",
      }],
    }),
    listAgentToolApprovals: vi.fn().mockResolvedValue({ items: [] }),
    decideAgentToolApproval: vi.fn(),
  },
}));

describe("OperationsPage", () => {
  it("renders the core summary and capability endpoints", async () => {
    const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
    renderWithI18n(
      <QueryClientProvider client={client}>
        <OperationsPage />
      </QueryClientProvider>,
    );

    expect(await screen.findByRole("heading", { name: "Operations" })).toBeTruthy();
    expect(await screen.findByText("14")).toBeTruthy();
    expect(await screen.findByText("provider adapter unavailable")).toBeTruthy();
    expect(await screen.findByText("2 capability gaps")).toBeTruthy();
  });
});
