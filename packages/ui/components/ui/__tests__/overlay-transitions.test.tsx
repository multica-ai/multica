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

afterEach(cleanup);

function slotClasses(slot: string): string[] {
  const el = document.querySelector(`[data-slot='${slot}']`);
  expect(el, `missing [data-slot='${slot}']`).not.toBeNull();
  return (el?.getAttribute("class") ?? "").split(/\s+/);
}

function expectAnimationOnlyDuration(classes: string[], expected: string[]) {
  for (const token of expected) {
    expect(classes).toContain(token);
  }
  // `duration-*` emits `transition-duration` against default `transition-property: all`;
  // `animation-duration-*` scopes timing strictly to keyframe animations without adding
  // `transition-none` (which would override caller-supplied arbitrary geometry transitions).
  expect(classes.filter((c) => /(^|:)duration-/.test(c))).toEqual([]);
  expect(classes).not.toContain("transition-none");
}

describe("keyframe-animated Base UI overlays", () => {
  it("uses animation-duration tokens on Dialog overlay and content", () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>Title</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    const expected = [
      "data-open:animation-duration-[var(--dialog-enter-duration)]",
      "data-closed:animation-duration-[var(--dialog-exit-duration)]",
    ];
    expectAnimationOnlyDuration(slotClasses("dialog-overlay"), expected);
    expectAnimationOnlyDuration(slotClasses("dialog-content"), expected);
  });

  it("preserves caller arbitrary geometry transitions on DialogContent", () => {
    render(
      <Dialog open>
        <DialogContent className="[transition-property:width,max-width,height] [transition-duration:200ms]">
          <DialogTitle>Create issue</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    const classes = slotClasses("dialog-content");
    expect(classes).toContain("[transition-property:width,max-width,height]");
    expect(classes).toContain("[transition-duration:200ms]");
    expect(classes).not.toContain("transition-none");
  });

  it("uses animation-duration tokens on AlertDialog overlay and content", () => {
    render(
      <AlertDialog open>
        <AlertDialogContent>
          <AlertDialogTitle>Confirm</AlertDialogTitle>
        </AlertDialogContent>
      </AlertDialog>,
    );
    const expected = [
      "data-open:animation-duration-[var(--dialog-enter-duration)]",
      "data-closed:animation-duration-[var(--dialog-exit-duration)]",
    ];
    expectAnimationOnlyDuration(slotClasses("alert-dialog-overlay"), expected);
    expectAnimationOnlyDuration(slotClasses("alert-dialog-content"), expected);
  });

  it("uses animation-duration-100 on Popover content", () => {
    render(
      <Popover open>
        <PopoverContent>Body</PopoverContent>
      </Popover>,
    );
    expectAnimationOnlyDuration(slotClasses("popover-content"), ["animation-duration-100"]);
  });

  it("uses animation-duration-100 on DropdownMenu content and sub-content", () => {
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
    expectAnimationOnlyDuration(slotClasses("dropdown-menu-content"), ["animation-duration-100"]);
    expectAnimationOnlyDuration(slotClasses("dropdown-menu-sub-content"), ["animation-duration-100"]);
  });

  it("uses animation-duration-100 on ContextMenu content", () => {
    render(
      <ContextMenu open>
        <ContextMenuContent>
          <ContextMenuItem>Copy</ContextMenuItem>
        </ContextMenuContent>
      </ContextMenu>,
    );
    expectAnimationOnlyDuration(slotClasses("context-menu-content"), ["animation-duration-100"]);
  });

  it("uses animation-duration-100 on Select content", () => {
    render(
      <Select open items={{ a: "Alpha" }} value="a">
        <SelectContent>
          <SelectItem value="a">Alpha</SelectItem>
        </SelectContent>
      </Select>,
    );
    expectAnimationOnlyDuration(slotClasses("select-content"), ["animation-duration-100"]);
  });

  it("uses animation-duration-100 on HoverCard content and preserves instant-close overrides", () => {
    render(
      <HoverCard open>
        <HoverCardContent className="data-closed:animation-duration-0">Preview</HoverCardContent>
      </HoverCard>,
    );
    const classes = slotClasses("hover-card-content");
    expectAnimationOnlyDuration(classes, ["animation-duration-100", "data-closed:animation-duration-0"]);
  });
});
