import { act, cleanup, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  CerebroImageContextMenu,
  inlineImageFigureStyle,
} from "./cerebro-image-context-menu";

const labels = {
  view: "View image",
  download: "Download",
  copyLink: "Copy link",
  size: "Size",
  small: "Small",
  medium: "Medium",
  fullWidth: "Full width",
  alignLeft: "Align left",
  alignCenter: "Align center",
  alignRight: "Align right",
  moveToBottom: "Move to bottom",
  delete: "Delete",
};

function renderMenu(widthPct: number | null = null) {
  const onWidthChange = vi.fn();
  const onAlignChange = vi.fn();

  render(
    <CerebroImageContextMenu
      trigger={<figure data-testid="image">Preview</figure>}
      widthPct={widthPct}
      align={null}
      editable
      canMoveToBottom
      labels={labels}
      onWidthChange={onWidthChange}
      onAlignChange={onAlignChange}
      onView={vi.fn()}
      onDownload={vi.fn()}
      onCopyLink={vi.fn()}
      onMoveToBottom={vi.fn()}
      onDelete={vi.fn()}
    />,
  );

  return { onWidthChange, onAlignChange };
}

afterEach(() => {
  cleanup();
  vi.useRealTimers();
});

describe("CerebroImageContextMenu", () => {
  it("offers phone-safe width presets from the desktop right-click menu", () => {
    const { onWidthChange } = renderMenu();

    fireEvent.contextMenu(screen.getByTestId("image"), {
      clientX: 120,
      clientY: 80,
    });

    for (const [label, width] of [
      ["Small", 33],
      ["Medium", 66],
      ["Full width", 100],
    ] as const) {
      const item = screen.getByRole("menuitem", { name: label });
      expect(item).toHaveClass("min-h-11");
      fireEvent.click(item);
      expect(onWidthChange).toHaveBeenLastCalledWith(width);

      if (label !== "Full width") {
        fireEvent.contextMenu(screen.getByTestId("image"), {
          clientX: 120,
          clientY: 80,
        });
      }
    }
  });

  it("opens the same menu after a 500 ms touch long-press", () => {
    vi.useFakeTimers();
    renderMenu(66);

    fireEvent.touchStart(screen.getByTestId("image"), {
      touches: [{ clientX: 120, clientY: 80 }],
    });
    act(() => vi.advanceTimersByTime(500));

    expect(screen.getByRole("menu")).toBeInTheDocument();
    expect(screen.getByRole("menuitem", { name: "Medium" })).toHaveAttribute(
      "aria-current",
      "true",
    );
    expect(screen.getByRole("menuitem", { name: "Move to bottom" })).toBeInTheDocument();
  });

  it("caps even malformed stored widths at the pane width", () => {
    expect(inlineImageFigureStyle(100)).toEqual({
      width: "100%",
      maxWidth: "100%",
    });
    expect(inlineImageFigureStyle(150)).toEqual({
      width: "100%",
      maxWidth: "100%",
    });
    expect(inlineImageFigureStyle(null)).toEqual({ maxWidth: "100%" });
  });
});
