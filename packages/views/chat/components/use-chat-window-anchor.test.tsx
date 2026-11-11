import { act, fireEvent, render } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useChatResize } from "./use-chat-resize";
import { useChatWindowAnchor } from "./use-chat-window-anchor";

vi.mock("@multica/core/chat", async () => {
  const { create } = await import("zustand");
  const store = create<{
    chatWidth: number;
    chatHeight: number;
    isExpanded: boolean;
    setExpanded: (isExpanded: boolean) => void;
    setChatSize: (chatWidth: number, chatHeight: number) => void;
  }>((set) => ({
    chatWidth: 420,
    chatHeight: 500,
    isExpanded: false,
    setExpanded: (isExpanded) => set({ isExpanded }),
    setChatSize: (chatWidth, chatHeight) => set({ chatWidth, chatHeight, isExpanded: false }),
  }));
  return { useChatStore: store, CHAT_MIN_W: 420, CHAT_MIN_H: 500 };
});

const CONTAINER = { left: 300, right: 1400 };
const WINDOW_WIDTH = 420;

function rect(left: number, width: number): DOMRect {
  return { left, right: left + width, width, top: 0, bottom: 0, height: 0, x: left, y: 0, toJSON: () => ({}) };
}

function Harness({
  isOpen,
  pressed,
  width = WINDOW_WIDTH,
  isResizing = false,
}: {
  isOpen: boolean;
  pressed: string | null;
  width?: number;
  isResizing?: boolean;
}) {
  const windowRef = useRef<HTMLDivElement>(null);
  const right = useChatWindowAnchor(windowRef, isOpen, width, isResizing);
  return (
    <>
      <div data-testid="container">
        <div ref={windowRef} data-testid="window" data-right={right} />
      </div>
      <button data-chat-anchor="a" aria-pressed={pressed === "a"} />
      <button data-chat-anchor="b" aria-pressed={pressed === "b"} />
    </>
  );
}

/** jsdom lays nothing out, so give the elements the hook measures a geometry. */
function layOut(anchors: Record<string, number>) {
  vi.spyOn(HTMLElement.prototype, "getBoundingClientRect").mockImplementation(function (this: HTMLElement) {
    if (this.dataset.testid === "container") return rect(CONTAINER.left, CONTAINER.right - CONTAINER.left);
    const anchor = this.dataset.chatAnchor;
    if (anchor && anchor in anchors) return rect(anchors[anchor]!, 120);
    return rect(0, 0);
  });
}

const rightOf = (view: ReturnType<typeof render>) => Number(view.getByTestId("window").dataset.right);

afterEach(() => {
  vi.restoreAllMocks();
});

describe("useChatWindowAnchor", () => {
  it("lines the window's left edge up with the pressed control", () => {
    layOut({ a: 700, b: 1200 });
    const view = render(<Harness isOpen pressed="a" />);

    // left = 700, so right = 1400 - 700 - 420
    expect(rightOf(view)).toBe(280);
  });

  it("pulls the window back inside the container when the control is near the edge", () => {
    layOut({ a: 700, b: 1200 });
    const view = render(<Harness isOpen pressed="b" />);

    expect(rightOf(view)).toBe(8);
  });

  it("never starts left of the container", () => {
    layOut({ a: 100, b: 1200 });
    const view = render(<Harness isOpen pressed="a" />);

    // left = 300 + 8
    expect(rightOf(view)).toBe(1400 - 308 - WINDOW_WIDTH);
  });

  it("rests in the corner when no control is pressed or the bar is not laid out", () => {
    layOut({});
    const view = render(<Harness isOpen pressed={null} />);
    expect(rightOf(view)).toBe(8);
    view.unmount();

    expect(rightOf(render(<Harness isOpen pressed="a" />))).toBe(8);
  });

  it("moves when the bar opens another chat, and stays put otherwise", () => {
    const frames: FrameRequestCallback[] = [];
    vi.spyOn(window, "requestAnimationFrame").mockImplementation((cb) => frames.push(cb));
    layOut({ a: 700, b: 900 });
    const view = render(<Harness isOpen pressed="a" />);
    expect(rightOf(view)).toBe(280);

    // The pressed control changing on its own (a chip appearing for the chat
    // just started) must not move the window.
    view.rerender(<Harness isOpen pressed="b" />);
    expect(rightOf(view)).toBe(280);

    act(() => {
      view.container.querySelector<HTMLElement>('[data-chat-anchor="b"]')!.click();
      frames.forEach((cb) => cb(0));
    });
    expect(rightOf(view)).toBe(1400 - 900 - WINDOW_WIDTH);
  });

  it("places the window again when its size changes, inside the container", () => {
    layOut({ a: 700, b: 1200 });
    const view = render(<Harness isOpen pressed="a" />);
    expect(rightOf(view)).toBe(280);

    // Expanded: no room right of the control, so it sits against the right inset.
    view.rerender(<Harness isOpen pressed="a" width={990} />);
    expect(rightOf(view)).toBe(8);

    // Restored to a size other than the one it opened at: above its control again.
    view.rerender(<Harness isOpen pressed="a" width={360} />);
    expect(rightOf(view)).toBe(1400 - 700 - 360);
  });

  it("holds the right edge while the left edge is dragged, until the container stops it", () => {
    layOut({ a: 700, b: 1200 });
    const view = render(<Harness isOpen pressed="a" />);

    view.rerender(<Harness isOpen pressed="a" width={600} isResizing />);
    expect(rightOf(view)).toBe(280);

    view.rerender(<Harness isOpen pressed="a" width={990} isResizing />);
    // left held at the container's inset: 1400 - right - 990 = 308
    expect(rightOf(view)).toBe(1100 - 8 - 990);

    // Letting go does not move it.
    view.rerender(<Harness isOpen pressed="a" width={990} />);
    expect(rightOf(view)).toBe(1100 - 8 - 990);
  });
});

describe("useChatWindowAnchor with useChatResize", () => {
  function Window() {
    const ref = useRef<HTMLDivElement>(null);
    const { renderWidth, toggleExpand } = useChatResize(ref);
    const right = useChatWindowAnchor(ref, true, renderWidth);
    return (
      <>
        <div data-testid="container">
          <div ref={ref} data-testid="window" style={{ width: renderWidth, right }}>
            <button onClick={toggleExpand}>Expand</button>
          </div>
        </div>
        <button data-chat-anchor="a" aria-pressed="true" />
      </>
    );
  }

  it("expanding a window opened from a chip on the left keeps it inside the container", () => {
    vi.spyOn(HTMLElement.prototype, "clientWidth", "get").mockReturnValue(1100);
    vi.spyOn(HTMLElement.prototype, "clientHeight", "get").mockReturnValue(800);
    layOut({ a: 700 });
    const view = render(<Window />);
    const win = view.getByTestId("window");
    const left = () => 1400 - Number.parseFloat(win.style.right) - Number.parseFloat(win.style.width);
    expect(left()).toBe(700);

    fireEvent.click(view.getByRole("button", { name: "Expand" }));
    expect(Number.parseFloat(win.style.width)).toBe(990);
    expect(left()).toBeGreaterThanOrEqual(308);

    fireEvent.click(view.getByRole("button", { name: "Expand" }));
    expect(left()).toBe(700);
  });
});
