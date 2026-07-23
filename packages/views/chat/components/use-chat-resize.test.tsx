import { fireEvent, render } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { ChatResizeHandles } from "./chat-resize-handles";
import { useChatResize } from "./use-chat-resize";

// useChatResize only needs the size fields and setters from the chat store.
vi.mock("@multica/core/chat", () => {
  const state = {
    chatWidth: 360,
    chatHeight: 480,
    isExpanded: false,
    setChatSize: vi.fn(),
    setExpanded: vi.fn(),
  };
  const useChatStore = Object.assign(
    (selector?: (s: typeof state) => unknown) =>
      selector ? selector(state) : state,
    { getState: () => state },
  );
  return { CHAT_MIN_W: 320, CHAT_MIN_H: 400, useChatStore };
});

function Harness() {
  const ref = useRef<HTMLDivElement>(null);
  const { startDrag } = useChatResize(ref);
  return (
    <div ref={ref} style={{ position: "relative" }}>
      <ChatResizeHandles onDragStart={startDrag} />
    </div>
  );
}

const POINTER_ID = 11;

function startLeftDrag(container: HTMLElement) {
  // The left edge handle is the one with the col-resize cursor.
  const handle = container.querySelector<HTMLElement>(".cursor-col-resize")!;
  const setPointerCapture = vi.fn();
  const releasePointerCapture = vi.fn();
  // jsdom elements don't implement the pointer-capture API.
  handle.setPointerCapture = setPointerCapture;
  handle.hasPointerCapture = vi.fn(() => true);
  handle.releasePointerCapture = releasePointerCapture;

  fireEvent.pointerDown(handle, {
    button: 0,
    isPrimary: true,
    pointerId: POINTER_ID,
    clientX: 100,
    clientY: 100,
  });

  return { handle, setPointerCapture, releasePointerCapture };
}

describe("chat resize cursor lock cleanup", () => {
  beforeEach(() => {
    document.documentElement.removeAttribute("data-chat-resizing");
  });

  afterEach(() => {
    vi.restoreAllMocks();
    document.documentElement.removeAttribute("data-chat-resizing");
  });

  it("locks the resize cursor on drag start and captures the pointer", () => {
    const { container } = render(<Harness />);
    const { setPointerCapture } = startLeftDrag(container);

    expect(setPointerCapture).toHaveBeenCalledWith(POINTER_ID);
    expect(document.documentElement).toHaveAttribute(
      "data-chat-resizing",
      "left",
    );
  });

  it("clears the lock and releases capture on pointerup", () => {
    const { container } = render(<Harness />);
    const { releasePointerCapture } = startLeftDrag(container);

    fireEvent.pointerUp(document, { pointerId: POINTER_ID });

    expect(document.documentElement).not.toHaveAttribute("data-chat-resizing");
    expect(releasePointerCapture).toHaveBeenCalledWith(POINTER_ID);
  });

  it("clears the lock on pointercancel (no pointerup)", () => {
    const { container } = render(<Harness />);
    startLeftDrag(container);

    fireEvent.pointerCancel(document, { pointerId: POINTER_ID });

    expect(document.documentElement).not.toHaveAttribute("data-chat-resizing");
  });

  it("clears the lock on lostpointercapture (no pointerup)", () => {
    const { container } = render(<Harness />);
    const { handle } = startLeftDrag(container);

    fireEvent.lostPointerCapture(handle, { pointerId: POINTER_ID });

    expect(document.documentElement).not.toHaveAttribute("data-chat-resizing");
  });

  it("clears the lock when the window loses focus mid-drag", () => {
    const { container } = render(<Harness />);
    startLeftDrag(container);

    fireEvent.blur(window);

    expect(document.documentElement).not.toHaveAttribute("data-chat-resizing");
  });
});
