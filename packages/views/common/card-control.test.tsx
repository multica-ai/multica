/**
 * Controls inside a board card that is both a link and a drag source.
 *
 * Menu and popover are deliberately NOT mocked: the regression these guard
 * against (MUL-9295) only shows with the real Base UI trigger, which opens a
 * menu on `mousedown`. user-event follows the Pointer Events rule that a
 * cancelled `pointerdown` suppresses `mousedown`, so it reproduces the bug
 * where `fireEvent` would not.
 */
import { describe, expect, it, vi } from "vitest";
import { fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@multica/ui/components/ui/dropdown-menu";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { AppLink } from "../navigation/app-link";
import { NavigationProvider } from "../navigation/context";
import type { NavigationAdapter } from "../navigation/types";
import { CardControl } from "./card-control";

function makeAdapter(push: NavigationAdapter["push"]): NavigationAdapter {
  return {
    push,
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => `https://app.example${p}`,
  };
}

function renderInCard(control: React.ReactNode) {
  const push = vi.fn();
  const card = {
    onPointerDown: vi.fn(),
    onMouseDown: vi.fn(),
    onClick: vi.fn(),
  };
  render(
    <NavigationProvider value={makeAdapter(push)}>
      <div data-testid="card" {...card}>
        <AppLink href="/acme/issues/issue-1">
          <span>Card title</span>
          <CardControl>{control}</CardControl>
        </AppLink>
      </div>
    </NavigationProvider>,
  );
  return { push, card };
}

describe("CardControl", () => {
  it("opens a dropdown menu inside a card link and picks an item without navigating", async () => {
    const user = userEvent.setup();
    const onSelect = vi.fn();
    const { push, card } = renderInCard(
      <DropdownMenu>
        <DropdownMenuTrigger render={<button type="button">Status</button>} />
        <DropdownMenuContent>
          <DropdownMenuItem onClick={onSelect}>Done</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    await user.click(screen.getByRole("button", { name: "Status" }));
    await user.click(await screen.findByRole("menuitem", { name: "Done" }));

    expect(onSelect).toHaveBeenCalledTimes(1);
    expect(push).not.toHaveBeenCalled();
    expect(card.onPointerDown).not.toHaveBeenCalled();
    expect(card.onMouseDown).not.toHaveBeenCalled();
    expect(card.onClick).not.toHaveBeenCalled();
  });

  it("opens a popover inside a card link without navigating", async () => {
    const user = userEvent.setup();
    const { push, card } = renderInCard(
      <Popover>
        <PopoverTrigger render={<button type="button">Assignee</button>} />
        <PopoverContent>Assignee options</PopoverContent>
      </Popover>,
    );

    await user.click(screen.getByRole("button", { name: "Assignee" }));

    expect(await screen.findByText("Assignee options")).toBeInTheDocument();
    expect(push).not.toHaveBeenCalled();
    expect(card.onPointerDown).not.toHaveBeenCalled();
    expect(card.onClick).not.toHaveBeenCalled();
  });

  it("keeps a press on the control from natively dragging the card link", () => {
    renderInCard(<button type="button">Status</button>);
    const link = screen.getByRole("link");

    fireEvent.pointerDown(screen.getByRole("button", { name: "Status" }));
    expect(fireEvent.dragStart(link)).toBe(false);

    fireEvent.pointerUp(document);
    expect(fireEvent.dragStart(link)).toBe(true);
  });

  it("leaves the rest of the card navigating and receiving presses", async () => {
    const user = userEvent.setup();
    const { push, card } = renderInCard(<button type="button">Status</button>);

    await user.click(screen.getByText("Card title"));

    expect(push).toHaveBeenCalledWith("/acme/issues/issue-1");
    expect(card.onPointerDown).toHaveBeenCalled();
  });
});
