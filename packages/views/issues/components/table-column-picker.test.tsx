import { beforeEach, describe, expect, it, vi } from "vitest";
import { screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import type { MouseEventHandler, ReactNode } from "react";
import type { IssueProperty } from "@multica/core/types";
import { renderWithI18n } from "../../test/i18n";
import { TableColumnPicker } from "./table-view";

const { toggleTableColumn } = vi.hoisted(() => ({
  toggleTableColumn: vi.fn(),
}));

vi.mock("@multica/core/issues/stores/view-store-context", () => ({
  useViewStore: (selector: (state: unknown) => unknown) =>
    selector({
      tableColumns: [{ key: "title" }],
      toggleTableColumn,
    }),
}));

vi.mock("@multica/ui/components/ui/dropdown-menu", () => ({
  DropdownMenu: ({ children }: { children: ReactNode }) => children,
  DropdownMenuContent: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuGroup: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuItem: ({
    children,
    disabled,
    onClick,
  }: {
    children: ReactNode;
    disabled?: boolean;
    onClick?: MouseEventHandler<HTMLButtonElement>;
  }) => (
    <button type="button" role="menuitem" disabled={disabled} onClick={onClick}>
      {children}
    </button>
  ),
  DropdownMenuLabel: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuRadioGroup: ({ children }: { children: ReactNode }) => children,
  DropdownMenuRadioItem: ({ children }: { children: ReactNode }) => (
    <div>{children}</div>
  ),
  DropdownMenuSeparator: () => <hr />,
  DropdownMenuTrigger: ({ render }: { render: ReactNode }) => render,
}));

const environmentProperty: IssueProperty = {
  id: "property-environment",
  workspace_id: "workspace-1",
  name: "Environment",
  type: "select",
  config: { options: [] },
  position: 0,
  archived: false,
  created_at: "2026-07-15T00:00:00Z",
  updated_at: "2026-07-15T00:00:00Z",
};

function renderPicker() {
  renderWithI18n(
    <TableColumnPicker
      properties={[environmentProperty]}
      trigger={<button type="button">Add column</button>}
    />,
  );
}

describe("TableColumnPicker", () => {
  beforeEach(() => {
    toggleTableColumn.mockClear();
  });

  it("toggles a system column when its menu item is clicked", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Add column" }));
    await user.click(screen.getByRole("menuitem", { name: "Project" }));

    expect(toggleTableColumn).toHaveBeenCalledWith("project");
  });

  it("toggles a custom-property column when its menu item is clicked", async () => {
    const user = userEvent.setup();
    renderPicker();

    await user.click(screen.getByRole("button", { name: "Add column" }));
    await user.click(screen.getByRole("menuitem", { name: "Environment" }));

    expect(toggleTableColumn).toHaveBeenCalledWith("property:property-environment");
  });
});
