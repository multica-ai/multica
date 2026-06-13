import { beforeEach, describe, expect, it, vi } from "vitest";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import type { ButtonHTMLAttributes, ReactNode } from "react";

const mockCerebroRequest = vi.hoisted(() => vi.fn());
vi.mock("@multica/core/api", async () => {
  const actual = await vi.importActual<typeof import("@multica/core/api")>(
    "@multica/core/api",
  );
  return { ...actual, api: { cerebroRequest: mockCerebroRequest } };
});
vi.mock("@multica/core/hooks", () => ({ useWorkspaceId: () => "ws-1" }));

// Base UI dropdown menus render through a portal and are awkward to drive in
// jsdom, so we flatten them — the trigger and items render inline as plain
// buttons (the established pattern across the repo's view tests). The item
// forwards `disabled` so we can assert futile-choice gating.
vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuTrigger: ({
    children,
    ...props
  }: { children: ReactNode } & ButtonHTMLAttributes<HTMLButtonElement>) => (
    <button type="button" {...props}>
      {children}
    </button>
  ),
  DropdownMenuContent: ({ children }: { children: ReactNode }) => <>{children}</>,
  DropdownMenuItem: ({
    children,
    onClick,
    disabled,
  }: {
    children: ReactNode;
    onClick?: () => void;
    disabled?: boolean;
  }) => (
    <button type="button" role="menuitem" onClick={onClick} disabled={disabled}>
      {children}
    </button>
  ),
}));

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

  it("disables 'Tillad alle' and the looser per-endpoint choices when the connection floor is Deny", () => {
    renderSheet(connRow("deny", { decided_by: "workspace" }));
    expect(screen.getByRole("button", { name: /Tillad alle/ })).toBeDisabled();
    const tool = screen.getByTestId("connection-tool-lookup_order");
    // The pill dropdown lists the choices; looser-than-floor ones are disabled.
    expect(within(tool).getByRole("menuitem", { name: "Allow" })).toBeDisabled();
    expect(within(tool).getByRole("menuitem", { name: "Ask" })).toBeDisabled();
    // Tightening + clearing stay available.
    expect(within(tool).getByRole("menuitem", { name: "Deny" })).toBeEnabled();
    expect(within(tool).getByRole("menuitem", { name: /Inherit/ })).toBeEnabled();
  });

  it("leaves all choices enabled and shows no banner when the connection allows", () => {
    renderSheet(connRow("allow"));
    expect(screen.queryByTestId("connection-blocked-banner")).not.toBeInTheDocument();
    expect(screen.getByRole("button", { name: /Tillad alle/ })).toBeEnabled();
    const tool = screen.getByTestId("connection-tool-lookup_order");
    expect(within(tool).getByRole("menuitem", { name: "Allow" })).toBeEnabled();
  });

  it("renders one decision pill per endpoint showing its effective verdict", () => {
    renderSheet(connRow("allow"));
    const tool = screen.getByTestId("connection-tool-lookup_order");
    // Single compact control per row (the app-wide pill), not a 4-button row.
    expect(within(tool).getByRole("button", { name: /^Decision:/ })).toBeInTheDocument();
  });

  it("writes the chosen setting for the endpoint when a menu choice is clicked", async () => {
    const user = userEvent.setup();
    renderSheet(connRow("allow"));
    const tool = screen.getByTestId("connection-tool-lookup_order");
    await user.click(within(tool).getByRole("menuitem", { name: "Deny" }));
    expect(mockCerebroRequest).toHaveBeenCalled();
  });
});
