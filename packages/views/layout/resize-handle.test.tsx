import { fireEvent, render } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ResizeHandle } from "@multica/ui/components/ui/resize-handle";
import type { ResizeAxis } from "@multica/ui/hooks/use-resize-gesture";

function renderHandle(
  props: Partial<React.ComponentProps<typeof ResizeHandle>> = {},
) {
  const onResize = vi.fn();
  const onResizeEnd = vi.fn();
  const utils = render(
    <ResizeHandle
      data-testid="handle"
      axis="x"
      onResize={onResize}
      onResizeEnd={onResizeEnd}
      {...props}
    />,
  );
  const handle = utils.getByTestId("handle");
  handle.setPointerCapture = vi.fn();
  handle.hasPointerCapture = vi.fn(() => true);
  handle.releasePointerCapture = vi.fn();
  return { ...utils, handle, onResize, onResizeEnd };
}

function startDrag(handle: HTMLElement, pointerId = 1) {
  fireEvent.pointerDown(handle, { button: 0, clientX: 0, clientY: 0, isPrimary: true, pointerId });
}

// jsdom turns an exception thrown inside an event listener into an uncaught
// error on window rather than propagating it to the dispatcher, so a throwing
// callback can only be observed this way.
function swallowUncaughtErrors() {
  const seen: string[] = [];
  const onError = (event: ErrorEvent) => {
    seen.push(event.error?.message ?? event.message);
    event.preventDefault();
  };
  window.addEventListener("error", onError);
  afterEach(() => window.removeEventListener("error", onError));
  return { errors: () => seen.join("\n") };
}

describe("ResizeHandle", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-resize-axis");
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // The whole point of the primitive: a call site cannot pick the wrong
  // cursor token. col-resize/row-resize are the Safari-only branch of
  // react-resizable-panels and must never appear on a Multica runtime.
  it.each([
    ["x", "cursor-ew-resize"],
    ["y", "cursor-ns-resize"],
    ["xy", "cursor-nwse-resize"],
  ] as [ResizeAxis, string][])("maps axis %s to %s", (axis, expected) => {
    const { handle } = renderHandle({ axis });
    expect(handle).toHaveClass(expected);
    expect(handle.className).not.toMatch(/cursor-(col|row)-resize/);
  });

  it.each([
    ["x", "x"],
    ["y", "y"],
    ["xy", "xy"],
  ] as [ResizeAxis, string][])(
    "locks the document cursor to axis %s for the duration of the drag",
    (axis, expected) => {
      const { handle } = renderHandle({ axis });

      startDrag(handle);
      expect(document.documentElement).toHaveAttribute("data-resize-axis", expected);
      expect(handle).toHaveAttribute("data-resizing", "true");

      fireEvent.pointerUp(document, { pointerId: 1 });
      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(handle).not.toHaveAttribute("data-resizing");
    },
  );

  it("reports raw pointer deltas and commits on pointerup", () => {
    const { handle, onResize, onResizeEnd } = renderHandle();

    startDrag(handle);
    fireEvent.pointerMove(document, { clientX: 40, clientY: 12, pointerId: 1 });

    expect(onResize).toHaveBeenCalledTimes(1);
    expect(onResize.mock.calls[0]![0]).toEqual({ dx: 40, dy: 12 });

    fireEvent.pointerUp(document, { pointerId: 1 });
    expect(onResizeEnd).toHaveBeenCalledWith("commit");
  });

  it("does not report movement below the threshold", () => {
    const { handle, onResize } = renderHandle({ threshold: 10 });

    startDrag(handle);
    fireEvent.pointerMove(document, { clientX: 4, clientY: 0, pointerId: 1 });
    expect(onResize).not.toHaveBeenCalled();

    fireEvent.pointerMove(document, { clientX: 12, clientY: 0, pointerId: 1 });
    expect(onResize).toHaveBeenCalledTimes(1);
  });

  it("aborts the gesture when onResizeStart returns false", () => {
    const { handle, onResize } = renderHandle({ onResizeStart: () => false });

    startDrag(handle);
    expect(document.documentElement).not.toHaveAttribute("data-resize-axis");

    fireEvent.pointerMove(document, { clientX: 40, pointerId: 1 });
    expect(onResize).not.toHaveBeenCalled();
  });

  it("ignores non-primary and secondary buttons", () => {
    const { handle } = renderHandle();

    fireEvent.pointerDown(handle, { button: 2, isPrimary: true, pointerId: 1 });
    expect(document.documentElement).not.toHaveAttribute("data-resize-axis");

    fireEvent.pointerDown(handle, { button: 0, isPrimary: false, pointerId: 1 });
    expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
  });

  // A leaked data-resize-axis locks the cursor for the entire document until
  // reload, so every teardown path is covered explicitly.
  describe("cursor lock teardown", () => {
    it("releases on pointercancel", () => {
      const { handle, onResizeEnd } = renderHandle();
      startDrag(handle);
      fireEvent.pointerCancel(document, { pointerId: 1 });

      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(onResizeEnd).toHaveBeenCalledWith("cancel");
    });

    it("releases on lostpointercapture", () => {
      const { handle, onResizeEnd } = renderHandle();
      startDrag(handle);
      fireEvent(handle, new PointerEvent("lostpointercapture", { pointerId: 1 }));

      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(onResizeEnd).toHaveBeenCalledWith("cancel");
    });

    it("releases on window blur", () => {
      const { handle, onResizeEnd } = renderHandle();
      startDrag(handle);
      fireEvent.blur(window);

      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(onResizeEnd).toHaveBeenCalledWith("cancel");
    });

    it("releases on unmount mid-drag", () => {
      const { handle, unmount, onResizeEnd } = renderHandle();
      startDrag(handle);
      unmount();

      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(onResizeEnd).toHaveBeenCalledWith("cancel");
    });

    it("releases when disabled flips mid-drag", () => {
      const onResize = vi.fn();
      const onResizeEnd = vi.fn();
      const { rerender, getByTestId } = render(
        <ResizeHandle data-testid="handle" axis="x" onResize={onResize} onResizeEnd={onResizeEnd} />,
      );
      const handle = getByTestId("handle");
      handle.setPointerCapture = vi.fn();
      handle.hasPointerCapture = vi.fn(() => true);
      handle.releasePointerCapture = vi.fn();

      startDrag(handle);
      expect(document.documentElement).toHaveAttribute("data-resize-axis", "x");

      rerender(
        <ResizeHandle data-testid="handle" axis="x" disabled onResize={onResize} onResizeEnd={onResizeEnd} />,
      );

      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(onResizeEnd).toHaveBeenCalledWith("cancel");

      fireEvent.pointerMove(document, { clientX: 40, pointerId: 1 });
      expect(onResize).not.toHaveBeenCalled();
    });

    // The error still propagates (a throwing caller is a real bug and should
    // reach error reporting); what must not happen is the document staying
    // cursor-locked because of it.
    it("releases when the caller's onResize throws", () => {
      const { errors } = swallowUncaughtErrors();
      const onResize = vi.fn(() => {
        throw new Error("boom");
      });
      const { handle } = renderHandle({ onResize });

      startDrag(handle);
      fireEvent.pointerMove(document, { clientX: 40, pointerId: 1 });

      expect(errors()).toContain("boom");
      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(handle).not.toHaveAttribute("data-resizing");
    });

    it("releases when the caller's onResizeEnd throws", () => {
      const { errors } = swallowUncaughtErrors();
      const onResizeEnd = vi.fn(() => {
        throw new Error("commit failed");
      });
      const { handle } = renderHandle({ onResizeEnd });

      startDrag(handle);
      fireEvent.pointerUp(document, { pointerId: 1 });

      expect(errors()).toContain("commit failed");
      expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
      expect(handle).not.toHaveAttribute("data-resizing");
    });
  });

  describe("indicator", () => {
    it("shows a hover and active line by default", () => {
      const { handle } = renderHandle();
      expect(handle).toHaveClass("hover:after:bg-foreground/15");
      expect(handle).toHaveClass("data-[resizing=true]:after:bg-foreground/25");
    });

    it("can be turned off", () => {
      const { handle } = renderHandle({ indicator: "none" });
      expect(handle.className).not.toMatch(/after:bg-foreground/);
    });
  });
});
