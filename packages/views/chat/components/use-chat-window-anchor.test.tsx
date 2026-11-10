import { act, render } from "@testing-library/react";
import { useRef } from "react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useChatWindowAnchor } from "./use-chat-window-anchor";

const CONTAINER = { left: 300, right: 1400 };
const WINDOW_WIDTH = 420;

function rect(left: number, width: number): DOMRect {
  return { left, right: left + width, width, top: 0, bottom: 0, height: 0, x: left, y: 0, toJSON: () => ({}) };
}

function Harness({ isOpen, pressed }: { isOpen: boolean; pressed: string | null }) {
  const windowRef = useRef<HTMLDivElement>(null);
  const right = useChatWindowAnchor(windowRef, isOpen);
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
  vi.spyOn(HTMLElement.prototype, "offsetWidth", "get").mockReturnValue(WINDOW_WIDTH);
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
});
