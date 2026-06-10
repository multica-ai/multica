import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return { ...actual, api: { cerebroRequest: mockCerebroRequest } };
});
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

import { ConnectionConfigSheet } from "./connection-config-sheet";
import type { ToolPolicyRow } from "../core";

function connRow(
  setting: "allow" | "ask" | "deny",
  over: Partial<ToolPolicyRow["effective"]> = {},
): ToolPolicyRow {
  return {
    tool_key: "connection:customer-service",
    resource_pattern: "",
    title: "Customer Service",
    category: "Connections",
    source: "connection",
    managed_externally: false,
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting, decided_by: "", capped_by: "", reason: "", ...over },
    capped_by_groups: [],
  };
}

function toolRow(name: string): ToolPolicyRow {
  return {
    tool_key: "connection:customer-service",
    resource_pattern: name,
    title: name,
    category: "Customer Service",
    source: "connection-tool",
    managed_externally: false,
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null },
    effective: { setting: "deny", decided_by: "", capped_by: "", reason: "" },
    capped_by_groups: [],
  };
}

function renderSheet(connectionRow: ToolPolicyRow) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ConnectionConfigSheet
        open
        onOpenChange={() => {}}
        connectionKey="connection:customer-service"
        connectionLabel="Customer Service"
        connectionRow={connectionRow}
        toolRows={[toolRow("lookup_order"), toolRow("draft_reply")]}
        editLayer="agent"
        subjectId="agent-1"
      />
    </QueryClientProvider>,
  );
}

beforeEach(() => mockCerebroRequest.mockReset());

describe("ConnectionConfigSheet (TECH-3287 hul 1/6/7)", () => {
  it("shows a blocked banner when the connection is denied from a higher layer", () => {
    // Workspace denies the whole connection → the agent page can't loosen it.
    renderSheet(connRow("deny", { decided_by: "workspace", reason: "Denied by workspace" }));
    const banner = screen.getByTestId("connection-blocked-banner");
    expect(within(banner).getByText(/Hele forbindelsen er sat til/)).toBeInTheDocument();
    expect(within(banner).getByText(/Workspace/)).toBeInTheDocument();
  });

  it("disables 'Tillad alle' and per-tool Allow/Ask when the connection floor is Deny", () => {
    renderSheet(connRow("deny", { decided_by: "workspace" }));
    expect(screen.getByRole("button", { name: /Tillad alle/ })).toBeDisabled();
    const tool = screen.getByTestId("connection-tool-lookup_order");
    expect(within(tool).getByRole("button", { name: "Allow" })).toBeDisabled();
    expect(within(tool).getByRole("button", { name: "Ask" })).toBeDisabled();
    // Tightening + clearing stay available.
    expect(within(tool).getByRole("button", { name: "Deny" })).toBeEnabled();
    expect(within(tool).getByRole("button", { name: "Inherit" })).toBeEnabled();
  });

  it("leaves all controls enabled and shows no banner when the connection allows", () => {
    renderSheet(connRow("allow"));
    expect(screen.queryByTestId("connection-blocked-banner")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Tillad alle/ })).toBeEnabled();
    const tool = screen.getByTestId("connection-tool-lookup_order");
    expect(within(tool).getByRole("button", { name: "Allow" })).toBeEnabled();
  });
});
