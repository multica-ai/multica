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

// The catalog decision pill (CatalogDecisionControl) lives in a Radix Popover
// (portals + pointer APIs jsdom lacks). Flatten it the same way the table test
// does so the inline list's single pill is drivable.
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

import { ConnectionConfigSheet, ConnectionToolList } from "./connection-config-sheet";
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
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null, system: null },
    conditions: { workspace: null, runtime: null, agent: null, user: null, system: null },
    effective: { setting, decided_by: "", capped_by: "", reason: "", openable: false, ...over },
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
    layers: { workspace: null, runtime: null, agent: null, group: null, user: null, system: null },
    conditions: { workspace: null, runtime: null, agent: null, user: null, system: null },
    effective: { setting: "deny", decided_by: "", capped_by: "", reason: "", openable: false },
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
    expect(within(banner).getByText(/The whole connection is set to/)).toBeInTheDocument();
    expect(within(banner).getByText(/Workspace/)).toBeInTheDocument();
  });

  it("disables 'Allow all' and the looser per-endpoint choices when the connection floor is Deny", () => {
    renderSheet(connRow("deny", { decided_by: "workspace" }));
    expect(screen.getByRole("button", { name: /Allow all/ })).toBeDisabled();
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
    expect(screen.getByRole("button", { name: /Allow all/ })).toBeEnabled();
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

  // FIR-2640 review — mobile layout regression guards. The base SheetContent for
  // side="right" carries data-[side=right]:w-3/4 + sm:max-w-sm, which have higher
  // CSS specificity than a plain w-full; the sheet width MUST be overridden with
  // the same data-[side=right] prefix or it silently reverts to 75% on mobile.
  it("makes the sheet full width on mobile via the data-[side=right] override", () => {
    renderSheet(connRow("allow"));
    const content = document.querySelector('[data-slot="sheet-content"]');
    expect(content).not.toBeNull();
    // The specificity-matching override that beats the base w-3/4 on mobile.
    expect(content?.className).toContain("data-[side=right]:w-full");
    // A bare w-full (which the base out-specifies) must NOT be relied on.
    expect(content?.className).not.toMatch(/(^|\s)w-full(\s|$)/);
  });

  it("lets long endpoint names scroll horizontally instead of truncating", () => {
    renderSheet(connRow("allow"));
    // The scroll region scrolls both axes; its inner column is min-w-max so long
    // paths extend past the viewport and become reachable by horizontal scroll.
    const scroller = document.querySelector(".overflow-auto");
    expect(scroller).not.toBeNull();
    expect(scroller?.querySelector(".min-w-max")).not.toBeNull();
    // The endpoint name no longer truncates (which hid it on a narrow sheet).
    const tool = screen.getByTestId("connection-tool-lookup_order");
    expect(tool.querySelector(".truncate")).toBeNull();
    expect(tool.querySelector(".whitespace-nowrap")).not.toBeNull();
  });
});

// ConnectionToolList — the inline list under an expanded connection row in the
// capability catalog. FIR-2706 follow-up: each tool row renders the SAME single
// decision-with-When pill as repo and credential sub-rows (CatalogDecisionControl),
// with the sheet's tighten-only floor rule preserved.
function renderInlineList(connectionRow: ToolPolicyRow) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={qc}>
      <ConnectionToolList
        connectionKey="connection:customer-service"
        connectionRow={connectionRow}
        toolRows={[toolRow("lookup_order"), toolRow("draft_reply")]}
        editLayer="agent"
        subjectId="agent-1"
      />
    </QueryClientProvider>,
  );
}

describe("ConnectionToolList (FIR-2706 same-design)", () => {
  it("renders each tool with ONE Decision pill and no separate When button", () => {
    renderInlineList(connRow("allow"));
    const tool = screen.getByTestId("connection-tool-lookup_order");
    // Exactly one control on the bar: the catalog Decision pill. The old
    // standalone When button (condition-control-*) is gone — When now lives
    // inside the pill, identical to repo and credential sub-rows.
    expect(within(tool).getByRole("button", { name: /^Decision:/ })).toBeInTheDocument();
    expect(
      within(tool).queryByTestId("condition-control-connection:customer-service"),
    ).not.toBeInTheDocument();
  });

  it("writes the chosen setting scoped to the tool's resource_pattern", async () => {
    const user = userEvent.setup();
    renderInlineList(connRow("allow"));
    const tool = screen.getByTestId("connection-tool-lookup_order");
    await user.click(within(tool).getByRole("button", { name: /^Decision:/ }));
    await user.click(
      within(tool).getByTestId("catalog-decision-connection:customer-service-deny"),
    );
    const puts = mockCerebroRequest.mock.calls.filter(
      (c) => (c[1] as RequestInit | undefined)?.method === "PUT",
    );
    const body = JSON.parse((puts.at(-1)![1] as RequestInit).body as string);
    expect(body).toMatchObject({
      tool_key: "connection:customer-service",
      resource_pattern: "lookup_order",
      layer: "agent",
      subject_id: "agent-1",
      setting: "deny",
    });
  });

  it("disables the looser-than-floor choices inside the pill when the connection floor is Deny", async () => {
    const user = userEvent.setup();
    renderInlineList(connRow("deny", { decided_by: "workspace" }));
    const tool = screen.getByTestId("connection-tool-lookup_order");
    await user.click(within(tool).getByRole("button", { name: /^Decision:/ }));
    // Tighten-only (TECH-3287 hul 7): Allow/Ask are futile below a Deny floor.
    expect(
      within(tool).getByTestId("catalog-decision-connection:customer-service-allow"),
    ).toBeDisabled();
    expect(
      within(tool).getByTestId("catalog-decision-connection:customer-service-ask"),
    ).toBeDisabled();
    expect(
      within(tool).getByTestId("catalog-decision-connection:customer-service-deny"),
    ).toBeEnabled();
    expect(
      within(tool).getByTestId("catalog-decision-connection:customer-service-inherit"),
    ).toBeEnabled();
  });
});
