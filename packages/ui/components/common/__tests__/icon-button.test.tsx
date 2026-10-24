import type { ReactNode } from "react";
import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { IconButton } from "../icon-button";
import { TooltipHint } from "../tooltip-hint";
import { DeferredTooltip } from "../deferred-tooltip";
import { TooltipProvider } from "../../ui/tooltip";
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "../../ui/dropdown-menu";

afterEach(cleanup);

function renderWithTooltips(ui: ReactNode) {
  return render(<TooltipProvider delay={0}>{ui}</TooltipProvider>);
}

async function hover(element: HTMLElement) {
  await act(async () => {
    fireEvent.pointerEnter(element, { pointerType: "mouse" });
    fireEvent.mouseEnter(element);
    fireEvent.pointerMove(element, { pointerType: "mouse" });
    fireEvent.mouseMove(element);
  });
}

describe("IconButton", () => {
  it("names the button with its label and shows label and shortcut on hover", async () => {
    renderWithTooltips(
      <IconButton label="Toggle sidebar" shortcut={<kbd>⌘B</kbd>}>
        <svg />
      </IconButton>,
    );

    const button = screen.getByRole("button", { name: "Toggle sidebar" });
    await hover(button);

    expect(await screen.findByText("⌘B")).toBeInTheDocument();
    expect(screen.getAllByText("Toggle sidebar").length).toBeGreaterThan(0);
  });

  it("stays natively disabled when no reason is given", () => {
    renderWithTooltips(
      <IconButton label="Send" disabled>
        <svg />
      </IconButton>,
    );

    const button = screen.getByRole("button", { name: "Send" });
    expect(button).toBeDisabled();
    expect(button).not.toHaveAttribute("aria-describedby");
  });

  it("keeps a disabled button hoverable and blocks activation when it has a reason", async () => {
    const onClick = vi.fn();
    renderWithTooltips(
      <IconButton label="Send" disabled disabledReason="Upload in progress" onClick={onClick}>
        <svg />
      </IconButton>,
    );

    const button = screen.getByRole("button", { name: "Send" });
    expect(button).not.toBeDisabled();
    expect(button).toHaveAttribute("aria-disabled", "true");
    expect(button).toHaveAccessibleDescription("Upload in progress");

    fireEvent.click(button);
    expect(onClick).not.toHaveBeenCalled();

    await hover(button);
    const popup = document.querySelector("[data-slot='tooltip-content']");
    expect(popup).toHaveTextContent("Upload in progress");
    expect(popup).not.toHaveTextContent("Send");
  });

  it("ignores the reason while enabled", () => {
    const onClick = vi.fn();
    renderWithTooltips(
      <IconButton label="Send" disabledReason="Upload in progress" onClick={onClick}>
        <svg />
      </IconButton>,
    );

    const button = screen.getByRole("button", { name: "Send" });
    expect(button).not.toHaveAttribute("aria-disabled");
    expect(button).not.toHaveAccessibleDescription();
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();
  });

  it("works as a menu trigger's render target", async () => {
    renderWithTooltips(
      <DropdownMenu>
        <DropdownMenuTrigger
          render={
            <IconButton label="More actions">
              <svg />
            </IconButton>
          }
        />
        <DropdownMenuContent>
          <DropdownMenuItem>Rename</DropdownMenuItem>
        </DropdownMenuContent>
      </DropdownMenu>,
    );

    const trigger = screen.getByRole("button", { name: "More actions" });
    expect(trigger).toHaveAttribute("aria-haspopup", "menu");
    await act(async () => {
      fireEvent.click(trigger);
    });
    expect(await screen.findByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
  });
});

describe("TooltipHint", () => {
  it("keeps the child's element and name and shows the label on hover", async () => {
    const onClick = vi.fn();
    renderWithTooltips(
      <TooltipHint label="Copy link">
        <button type="button" aria-label="Copy link" className="custom" onClick={onClick}>
          <svg />
        </button>
      </TooltipHint>,
    );

    const button = screen.getByRole("button", { name: "Copy link" });
    expect(button).toHaveClass("custom");
    fireEvent.click(button);
    expect(onClick).toHaveBeenCalledOnce();

    await hover(button);
    expect(document.querySelector("[data-slot='tooltip-content']")).toHaveTextContent("Copy link");
  });

  it("renders the child alone when there is no label", () => {
    renderWithTooltips(
      <TooltipHint label={undefined}>
        <button type="button">Save</button>
      </TooltipHint>,
    );

    const button = screen.getByRole("button", { name: "Save" });
    expect(button).not.toHaveAttribute("data-slot", "tooltip-trigger");
  });
});

describe("DeferredTooltip", () => {
  it("wraps a menu trigger and stays hidden while its menu is open", async () => {
    vi.useFakeTimers();
    try {
      renderWithTooltips(
        <DropdownMenu>
          <DeferredTooltip
            content="Row actions"
            trigger={
              <DropdownMenuTrigger
                render={
                  <button type="button" aria-label="Row actions">
                    <svg />
                  </button>
                }
              />
            }
          />
          <DropdownMenuContent>
            <DropdownMenuItem>Rename</DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>,
      );

      const trigger = screen.getByRole("button", { name: "Row actions" });
      expect(document.querySelector("[data-slot='tooltip-content']")).toBeNull();

      fireEvent.pointerEnter(trigger, { pointerType: "mouse" });
      await act(async () => {
        vi.advanceTimersByTime(250);
      });
      expect(document.querySelector("[data-slot='tooltip-content']")).toHaveTextContent("Row actions");

      await act(async () => {
        fireEvent.click(trigger);
      });
      expect(screen.getByRole("menuitem", { name: "Rename" })).toBeInTheDocument();
      expect(trigger).toHaveAttribute("aria-expanded", "true");

      fireEvent.pointerLeave(trigger, { pointerType: "mouse" });
      fireEvent.pointerEnter(trigger, { pointerType: "mouse" });
      await act(async () => {
        vi.advanceTimersByTime(250);
      });
      const popup = document.querySelector("[data-slot='tooltip-content']");
      expect(popup === null || popup.getAttribute("data-open") === null).toBe(true);
    } finally {
      vi.useRealTimers();
    }
  });

  it("still shows on an expanded disclosure toggle that has no popup", async () => {
    vi.useFakeTimers();
    try {
      renderWithTooltips(
        <DeferredTooltip
          content="Collapse thread"
          trigger={
            <button type="button" aria-label="Collapse thread" aria-expanded="true">
              <svg />
            </button>
          }
        />,
      );

      fireEvent.pointerEnter(screen.getByRole("button", { name: "Collapse thread" }), { pointerType: "mouse" });
      await act(async () => {
        vi.advanceTimersByTime(250);
      });
      expect(document.querySelector("[data-slot='tooltip-content']")).toHaveTextContent("Collapse thread");
    } finally {
      vi.useRealTimers();
    }
  });
});
