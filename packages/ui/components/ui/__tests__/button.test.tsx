import { cleanup, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { Button } from "../button";
import { Dialog, DialogTrigger } from "../dialog";
import { DropdownMenu, DropdownMenuTrigger } from "../dropdown-menu";
import { Popover, PopoverTrigger } from "../popover";
import { Tooltip, TooltipProvider, TooltipTrigger } from "../tooltip";
import { IconButton } from "../../common/icon-button";

afterEach(cleanup);

describe("Button data attributes", () => {
  it("marks a plain button with its slot, variant and size", () => {
    render(
      <Button variant="outline" size="sm">
        Save
      </Button>,
    );
    const button = screen.getByRole("button", { name: "Save" });
    expect(button).toHaveAttribute("data-slot", "button");
    expect(button).toHaveAttribute("data-variant", "outline");
    expect(button).toHaveAttribute("data-size", "sm");
  });

  it.each([
    [
      "tooltip",
      <TooltipProvider key="tooltip">
        <Tooltip>
          <TooltipTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Open" />} />
        </Tooltip>
      </TooltipProvider>,
    ],
    [
      "dropdown-menu",
      <DropdownMenu key="dropdown-menu">
        <DropdownMenuTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Open" />} />
      </DropdownMenu>,
    ],
    [
      "popover",
      <Popover key="popover">
        <PopoverTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Open" />} />
      </Popover>,
    ],
    [
      "dialog",
      <Dialog key="dialog">
        <DialogTrigger render={<Button variant="ghost" size="icon-sm" aria-label="Open" />} />
      </Dialog>,
    ],
  ])("keeps data-slot=button when a %s trigger renders it", (trigger, ui) => {
    render(ui);
    const button = screen.getByRole("button", { name: "Open" });
    expect(button).toHaveAttribute("data-slot", "button");
    expect(button).toHaveAttribute("data-variant", "ghost");
    expect(button).toHaveAttribute("data-size", "icon-sm");
    expect(button).toHaveAttribute("data-trigger", trigger);
  });

  it("keeps data-slot=button on an IconButton that opens a popover", () => {
    render(
      <TooltipProvider>
        <Popover>
          <PopoverTrigger render={<IconButton label="Filter" />} />
        </Popover>
      </TooltipProvider>,
    );
    const button = screen.getByRole("button", { name: "Filter" });
    expect(button).toHaveAttribute("data-slot", "button");
    expect(button).toHaveAttribute("data-size", "icon-sm");
    expect(button).toHaveAttribute("data-trigger", "popover");
  });
});
