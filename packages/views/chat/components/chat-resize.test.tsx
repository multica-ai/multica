import { fireEvent, render, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const h = vi.hoisted(() => {
  const store = {
    chatWidth: 400,
    chatHeight: 500,
    isExpanded: false,
    setChatSize: vi.fn(),
    setExpanded: vi.fn(),
  };
  return { store };
});

vi.mock("@multica/core/chat", () => {
  const useChatStore = Object.assign(
    (selector: (s: typeof h.store) => unknown) => selector(h.store),
    { getState: () => h.store },
  );
  return { useChatStore, CHAT_MIN_W: 360, CHAT_MIN_H: 480 };
});

import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatResize } from "./use-chat-resize";

function renderHandles() {
  const onResizeStart = vi.fn();
  const onResize = vi.fn();
  const onResizeEnd = vi.fn();
  const { container } = render(
    <ChatResizeHandles
      onResizeStart={onResizeStart}
      onResize={onResize}
      onResizeEnd={onResizeEnd}
    />,
  );
  const handles = Array.from(
    container.querySelectorAll<HTMLElement>("[data-slot='resize-handle']"),
  );
  for (const handle of handles) {
    handle.setPointerCapture = vi.fn();
    handle.hasPointerCapture = vi.fn(() => true);
    handle.releasePointerCapture = vi.fn();
  }
  return { handles, onResizeStart, onResize, onResizeEnd };
}

describe("chat resize handles", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-resize-axis");
    h.store.chatWidth = 400;
    h.store.chatHeight = 500;
    h.store.isExpanded = false;
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  // Before the shared primitive these three used col-resize/row-resize/nw-resize,
  // none of which match what the rest of the product renders.
  it("uses the shared resize cursors for the left edge, top edge and corner", () => {
    const { handles } = renderHandles();

    expect(handles).toHaveLength(3);
    expect(handles[0]).toHaveClass("cursor-ew-resize");
    expect(handles[1]).toHaveClass("cursor-ns-resize");
    expect(handles[2]).toHaveClass("cursor-nwse-resize");

    for (const handle of handles) {
      expect(handle.className).not.toMatch(/cursor-(col|row|nw)-resize/);
      expect(handle).toHaveAttribute("aria-hidden");
    }
  });

  it("gives the two edges a hover indicator", () => {
    const { handles } = renderHandles();

    expect(handles[0]).toHaveClass("hover:after:bg-foreground/15");
    expect(handles[1]).toHaveClass("hover:after:bg-foreground/15");
    // A corner has no single edge to draw along.
    expect(handles[2]).toHaveClass("after:hidden");
  });

  it("locks the document cursor per axis while dragging and clears it after", () => {
    const { handles } = renderHandles();

    fireEvent.pointerDown(handles[1]!, { button: 0, isPrimary: true, pointerId: 3 });
    expect(document.documentElement).toHaveAttribute("data-resize-axis", "y");

    fireEvent.pointerUp(document, { pointerId: 3 });
    expect(document.documentElement).not.toHaveAttribute("data-resize-axis");
  });

  it("reports each handle's direction to the caller", () => {
    const { handles, onResize, onResizeStart, onResizeEnd } = renderHandles();

    fireEvent.pointerDown(handles[2]!, { button: 0, clientX: 0, clientY: 0, isPrimary: true, pointerId: 4 });
    expect(onResizeStart).toHaveBeenCalledTimes(1);

    fireEvent.pointerMove(document, { clientX: -30, clientY: -20, pointerId: 4 });
    expect(onResize).toHaveBeenCalledWith("corner", { dx: -30, dy: -20 });

    fireEvent.pointerUp(document, { pointerId: 4 });
    expect(onResizeEnd).toHaveBeenCalledTimes(1);
  });
});

describe("useChatResize", () => {
  beforeEach(() => {
    h.store.chatWidth = 400;
    h.store.chatHeight = 500;
    h.store.isExpanded = false;
    vi.clearAllMocks();
  });

  function setup() {
    const ref = { current: null } as React.RefObject<HTMLDivElement | null>;
    return renderHook(() => useChatResize(ref));
  }

  // The window is anchored bottom-right: dragging the left edge left (negative
  // dx) has to make it wider, not narrower.
  it("grows the window when the left edge is dragged away from the anchor", () => {
    const { result } = setup();

    result.current.handleResizeStart();
    result.current.handleResize("left", { dx: -50, dy: 0 });

    expect(h.store.setChatSize).toHaveBeenCalledWith(450, 500);
  });

  it("grows the window when the top edge is dragged up", () => {
    const { result } = setup();

    result.current.handleResizeStart();
    result.current.handleResize("top", { dx: 0, dy: -40 });

    expect(h.store.setChatSize).toHaveBeenCalledWith(400, 540);
  });

  it("moves both axes from the corner", () => {
    const { result } = setup();

    result.current.handleResizeStart();
    result.current.handleResize("corner", { dx: -25, dy: -35 });

    expect(h.store.setChatSize).toHaveBeenCalledWith(425, 535);
  });

  it("clamps to the minimum size", () => {
    const { result } = setup();

    result.current.handleResizeStart();
    result.current.handleResize("corner", { dx: 999, dy: 999 });

    expect(h.store.setChatSize).toHaveBeenCalledWith(360, 480);
  });

  it("ignores movement that arrives without a live gesture", () => {
    const { result } = setup();

    result.current.handleResize("left", { dx: -50, dy: 0 });
    expect(h.store.setChatSize).not.toHaveBeenCalled();

    result.current.handleResizeStart();
    result.current.handleResizeEnd();
    result.current.handleResize("left", { dx: -50, dy: 0 });
    expect(h.store.setChatSize).not.toHaveBeenCalled();
  });
});
