import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import { RowActionsMenu } from "./row-actions-menu";

describe("RowActionsMenu", () => {
  it("names its trigger so a screen reader can announce the row's actions", () => {
    render(
      <RowActionsMenu
        label="Chat actions"
        groups={[[{ key: "archive", icon: null, label: "Archive", onSelect: vi.fn() }]]}
      />,
    );

    expect(screen.getByRole("button", { name: "Chat actions" })).toBeInTheDocument();
  });

  it("runs the action the user picks", async () => {
    const onSelect = vi.fn();
    render(
      <RowActionsMenu
        label="Chat actions"
        groups={[[{ key: "archive", icon: null, label: "Archive", onSelect }]]}
      />,
    );

    fireEvent.click(screen.getByRole("button", { name: "Chat actions" }));
    fireEvent.click(await screen.findByRole("menuitem", { name: "Archive" }));

    expect(onSelect).toHaveBeenCalledTimes(1);
  });

  it("stays out of the row's own click handling", () => {
    // The trigger sits inside a row that selects on click. Opening the menu
    // must not also select the row.
    const onRowClick = vi.fn();
    render(
      <div onClick={onRowClick}>
        <RowActionsMenu
          label="Chat actions"
          groups={[[{ key: "archive", icon: null, label: "Archive", onSelect: vi.fn() }]]}
        />
      </div>,
    );

    fireEvent.click(screen.getByRole("button", { name: "Chat actions" }));

    expect(onRowClick).not.toHaveBeenCalled();
  });

  it("renders nothing when every group is empty", () => {
    // A running chat with no pending task has no action to offer; an empty
    // trigger would be a dead control.
    const { container } = render(<RowActionsMenu label="Chat actions" groups={[[], []]} />);

    expect(container).toBeEmptyDOMElement();
  });
});
