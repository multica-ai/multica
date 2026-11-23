import { cleanup, render } from "@testing-library/react";
import { afterEach, describe, expect, it } from "vitest";
import { AlertDialog, AlertDialogContent, AlertDialogTitle } from "../alert-dialog";
import { ContextMenu, ContextMenuContent, ContextMenuItem } from "../context-menu";
import { Dialog, DialogContent, DialogTitle } from "../dialog";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSub,
  DropdownMenuSubContent,
} from "../dropdown-menu";
import { HoverCard, HoverCardContent } from "../hover-card";
import { Popover, PopoverContent } from "../popover";
import { Select, SelectContent, SelectItem } from "../select";
import { Tooltip, TooltipContent } from "../tooltip";

afterEach(cleanup);

function slotClasses(slot: string): string[] {
  const el = document.querySelector(`[data-slot='${slot}']`);
  expect(el, `missing [data-slot='${slot}']`).not.toBeNull();
  return (el?.getAttribute("class") ?? "").split(/\s+/);
}

// Keyframe-animated Base UI overlays pass `duration-*` only to feed
// `--tw-duration` into tw-animate-css. Without `transition-none`, Tailwind v4
// also applies `transition-duration` to default `transition-property: all`,
// creating incidental CSS transitions (e.g. `scrollbar-color` in Chromium)
// that Base UI's unmount waits on even when animations are skipped
// (`prefers-reduced-motion: reduce`, `Select` align-trigger, instant tooltip).
describe("keyframe-animated Base UI overlays", () => {
  it("disables CSS transitions on Dialog overlay and content", () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>Title</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    expect(slotClasses("dialog-overlay")).toContain("transition-none");
    expect(slotClasses("dialog-content")).toContain("transition-none");
  });

  it("disables CSS transitions on AlertDialog overlay and content", () => {
    render(
      <AlertDialog open>
        <AlertDialogContent>
          <AlertDialogTitle>Confirm</AlertDialogTitle>
        </AlertDialogContent>
      </AlertDialog>,
    );
    expect(slotClasses("alert-dialog-overlay")).toContain("transition-none");
    expect(slotClasses("alert-dialog-content")).toContain("transition-none");
  });

  it("disables CSS transitions on Popover content", () => {
    render(
      <Popover open>
        <PopoverContent>Body</PopoverContent>
      </Popover>,
    );
    expect(slotClasses("popover-content")).toContain("transition-none");
  });

  it("disables CSS transitions on DropdownMenu content and sub-content", () => {
    render(
      <DropdownMenu open>
        <DropdownMenuContent>
          <DropdownMenuItem>One</DropdownMenuItem>
          <DropdownMenuSub open>
            <DropdownMenuSubContent>
              <DropdownMenuItem>Sub</DropdownMenuItem>
            </DropdownMenuSubContent>
          </DropdownMenuSub>
        </DropdownMenuContent>
      </DropdownMenu>,
    );
    expect(slotClasses("dropdown-menu-content")).toContain("transition-none");
    expect(slotClasses("dropdown-menu-sub-content")).toContain("transition-none");
  });

  it("disables CSS transitions on ContextMenu content", () => {
    render(
      <ContextMenu open>
        <ContextMenuContent>
          <ContextMenuItem>Copy</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>,
    );
    expect(slotClasses("context-menu-content")).toContain("transition-none");
  });

  it("disables CSS transitions on Select content", () => {
    render(
      <Select open items={{ a: "Alpha" }} value="a">
        <SelectContent>
          <SelectItem value="a">Alpha</SelectItem>
        </SelectContent>
      </Select>,
    );
    expect(slotClasses("select-content")).toContain("transition-none");
  });

  it("disables CSS transitions on HoverCard content", () => {
    render(
      <HoverCard open>
        <HoverCardContent>Preview</HoverCardContent>
      </HoverCard>,
    );
    expect(slotClasses("hover-card-content")).toContain("transition-none");
  });

  it("keeps CSS transitions disabled on Tooltip content", () => {
    render(
      <Tooltip open>
        <TooltipContent>Hint</TooltipContent>
      </Tooltip>,
    );
    expect(slotClasses("tooltip-content")).toContain("transition-none");
  });
});
