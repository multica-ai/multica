// @vitest-environment jsdom

import type { ReactElement, ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";

vi.mock("@multica/ui/components/ui/tooltip", () => ({
  Tooltip: ({ children }: { children: ReactNode }) => <>{children}</>,
  TooltipTrigger: ({ render }: { render: ReactElement }) => render,
  TooltipContent: ({ children }: { children: ReactNode }) => (
    <span data-testid="tooltip-content">{children}</span>
  ),
}));

import { McpRemoveButton, McpServerRow } from "./mcp-server-row";

describe("McpServerRow", () => {
  it("keeps concise tooltip text separate from row-specific accessible names", () => {
    render(
      <ul>
        <McpServerRow
          name="linear"
          transport="stdio"
          canManage
          labels={{
            rename: "Rename",
            renameAria: "Rename server",
            renameSave: "Save name",
            renameCancel: "Cancel rename",
            configure: "Edit configuration",
            configureAria: "Edit configuration",
            remove: "Delete",
            removeAria: "Delete MCP server",
          }}
          onRenameStart={vi.fn()}
          onConfigure={vi.fn()}
          onRemove={vi.fn()}
        />
      </ul>,
    );

    expect(
      screen.getAllByTestId("tooltip-content").map((node) => node.textContent),
    ).toEqual(["STDIO", "Rename", "Edit configuration", "Delete"]);
    expect(
      screen.getByRole("button", { name: "Rename server linear" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Edit configuration linear" }),
    ).toBeInTheDocument();
    expect(
      screen.getByRole("button", { name: "Delete MCP server linear" }),
    ).toBeInTheDocument();
  });

  // Regression: the configure action opens the server's configuration dialog,
  // so it must not wear the circular-arrows icon this repo uses everywhere for
  // "refresh" (MUL-7057). That reading survived because the workspace library
  // calls the same action "Replace configuration".
  it("marks the configure action with the configure icon, not a refresh icon", () => {
    render(
      <ul>
        <McpServerRow
          name="linear"
          transport="stdio"
          canManage
          labels={{
            rename: "Rename",
            renameAria: "Rename server",
            renameSave: "Save name",
            renameCancel: "Cancel rename",
            configure: "Replace configuration",
            configureAria: "Replace configuration",
            remove: "Delete",
            removeAria: "Delete MCP server",
          }}
          onRenameStart={vi.fn()}
          onConfigure={vi.fn()}
          onRemove={vi.fn()}
        />
      </ul>,
    );

    const icon = screen
      .getByRole("button", { name: "Replace configuration linear" })
      .querySelector("svg");

    expect(icon).toHaveClass("lucide-sliders-horizontal");
    expect(icon?.getAttribute("class")).not.toMatch(/refresh|rotate/i);
  });

  it("cancels inline rename when Escape is pressed", async () => {
    const user = userEvent.setup();
    const onCancel = vi.fn();
    render(
      <ul>
        <McpServerRow
          name="linear"
          transport="http"
          canManage
          rename={{
            draft: "linear-v2",
            error: "",
            pending: false,
            onChange: vi.fn(),
            onCancel,
            onSubmit: vi.fn(),
          }}
          labels={{
            rename: "Rename",
            renameAria: "Rename server",
            renameSave: "Save name",
            renameCancel: "Cancel rename",
            configure: "Edit configuration",
            configureAria: "Edit configuration",
            remove: "Delete",
            removeAria: "Delete MCP server",
          }}
          onRenameStart={vi.fn()}
          onConfigure={vi.fn()}
          onRemove={vi.fn()}
        />
      </ul>,
    );

    const input = screen.getByRole("textbox", { name: "Rename server" });
    await user.click(input);
    await user.keyboard("{Escape}");

    expect(onCancel).toHaveBeenCalledOnce();
  });
});

describe("McpRemoveButton", () => {
  it("does not repeat the assigned server name in its tooltip", () => {
    render(
      <McpRemoveButton
        ariaLabel="Remove shared-linear from this agent"
        tooltipLabel="Remove"
        onClick={vi.fn()}
      />,
    );

    expect(screen.getByTestId("tooltip-content")).toHaveTextContent("Remove");
    expect(screen.getByTestId("tooltip-content")).not.toHaveTextContent(
      "shared-linear",
    );
    expect(
      screen.getByRole("button", {
        name: "Remove shared-linear from this agent",
      }),
    ).toBeInTheDocument();
  });
});
