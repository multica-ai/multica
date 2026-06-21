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

// Flatten the portal-based dropdown menu so the decision pill is drivable in
// jsdom (the established pattern across the repo's view tests).
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

// The Condition editor lives in a Radix Popover (portals + pointer APIs jsdom
// lacks). Flatten it the same way the table test does so the When-control is
// present and drivable.
vi.mock("@multica/ui/components/ui/popover", async () => {
  const React = await import("react");
  const Ctx = React.createContext<{ open: boolean; onOpenChange: (v: boolean) => void }>({
    open: false,
    onOpenChange: () => {},
  });
  return {
    Popover: ({
      open,
      onOpenChange,
      children,
    }: {
      open?: boolean;
      onOpenChange?: (v: boolean) => void;
      children: ReactNode;
    }) => (
      <Ctx.Provider value={{ open: !!open, onOpenChange: onOpenChange ?? (() => {}) }}>
        {children}
      </Ctx.Provider>
    ),
    PopoverTrigger: ({
      children,
      onClick,
      ...props
    }: { children: ReactNode } & ButtonHTMLAttributes<HTMLButtonElement>) => {
      const { open, onOpenChange } = React.useContext(Ctx);
      return (
        <button
          type="button"
          {...props}
          onClick={(e) => {
            onClick?.(e);
            onOpenChange(!open);
          }}
        >
          {children}
        </button>
      );
    },
    PopoverContent: ({ children }: { children: ReactNode }) => {
      const { open } = React.useContext(Ctx);
      return open ? <div>{children}</div> : null;
    },
    PopoverTitle: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    PopoverDescription: ({ children }: { children: ReactNode }) => <div>{children}</div>,
    PopoverHeader: ({ children }: { children: ReactNode }) => <div>{children}</div>,
  };
});

import { FirtalRegistryDataSourceSheet } from "./firtal-registry-data-source-sheet";
import type { ToolPolicyRow } from "../core";

// A projected per-data-source row: tool_key=firtal_registry, resource_pattern=ds
// id, source=registry-data-source. `setting` is the agent-layer decision.
function dsRow(
  id: string,
  name: string,
  setting: "allow" | "deny",
): ToolPolicyRow {
  return {
    tool_key: "firtal_registry",
    resource_pattern: id,
    title: name,
    category: "Data sources",
    source: "registry-data-source",
    managed_externally: false,
    layers: { workspace: null, runtime: null, agent: setting, group: null, user: null, system: null },
    conditions: { workspace: null, runtime: null, agent: null, user: null, system: null },
    effective: { setting, decided_by: "agent", capped_by: "", reason: "" },
    capped_by_groups: [],
  };
}

function renderSheet(rows: ToolPolicyRow[]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <FirtalRegistryDataSourceSheet
        open
        onOpenChange={() => {}}
        toolKey="firtal_registry"
        toolLabel="Firtal Data Registry"
        sourceRows={rows}
        editLayer="agent"
        subjectId="agent-1"
      />
    </QueryClientProvider>,
  );
}

function lastPut() {
  const puts = mockCerebroRequest.mock.calls.filter(
    (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
  );
  const call = puts.at(-1);
  if (!call) return undefined;
  return JSON.parse((call[1] as RequestInit).body as string);
}

beforeEach(() => {
  mockCerebroRequest.mockReset();
  mockCerebroRequest.mockResolvedValue(undefined);
});

describe("FirtalRegistryDataSourceSheet (FIR-1609 Phase 5)", () => {
  it("renders one decision pill per data source, allowed and denied alike", () => {
    renderSheet([dsRow("ds-1", "Orders", "allow"), dsRow("ds-2", "Finance", "deny")]);
    const orders = screen.getByTestId("registry-data-source-ds-1");
    const finance = screen.getByTestId("registry-data-source-ds-2");
    expect(within(orders).getByText("Orders")).toBeInTheDocument();
    expect(within(orders).getByRole("button", { name: /^Decision:/ })).toBeInTheDocument();
    expect(within(finance).getByText("Finance")).toBeInTheDocument();
  });

  it("writes the chosen decision scoped to the data source id", async () => {
    const user = userEvent.setup();
    renderSheet([dsRow("ds-1", "Orders", "allow")]);
    const orders = screen.getByTestId("registry-data-source-ds-1");
    await user.click(within(orders).getByRole("menuitem", { name: "Deny" }));
    const body = lastPut();
    expect(body).toMatchObject({
      tool_key: "firtal_registry",
      resource_pattern: "ds-1",
      layer: "agent",
      subject_id: "agent-1",
      setting: "deny",
    });
  });

  it("offers a When (Condition) control on a source with a concrete rule", () => {
    renderSheet([dsRow("ds-1", "Orders", "allow")]);
    const orders = screen.getByTestId("registry-data-source-ds-1");
    // ConditionControl renders an editable trigger (the WHEN layer) beside the
    // decision pill once the row carries a concrete allow/ask/deny on this layer.
    expect(
      within(orders).getByRole("button", { name: /when|condition|add|vilkår/i }),
    ).toBeTruthy();
  });

  it("shows get_schema/execute preset action chips for a data source (no host)", async () => {
    const user = userEvent.setup();
    renderSheet([dsRow("ds-1", "Orders", "allow")]);
    const orders = screen.getByTestId("registry-data-source-ds-1");
    await user.click(within(orders).getByTestId("condition-control-firtal_registry"));
    const editor = within(await screen.findByTestId("condition-editor-firtal_registry"));
    expect(editor.getByLabelText("Action get_schema")).toBeInTheDocument();
    expect(editor.getByLabelText("Action execute")).toBeInTheDocument();
    // A registry data source is not host-bound → no host allow-list section.
    expect(editor.queryByLabelText("Add host")).not.toBeInTheDocument();
  });
});
