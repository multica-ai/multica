import { useRef, useState } from "react";
import { act, render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useFloatPosition } from "./use-float-position";

// JEH-1065 — Bug 1 regression test. The pinned input MUST keep its child
// component instance alive across pin/unpin so the Tiptap editor draft and
// caret survive. The implementation achieves this by leaving the editor in
// the SAME React tree position on every render and only toggling inline
// styling on the wrapper. This test asserts the property directly with a
// stateful Counter as the canary.

function Counter() {
  const [n, setN] = useState(0);
  return (
    <button type="button" data-testid="counter" onClick={() => setN((v) => v + 1)}>
      n={n}
    </button>
  );
}

function Harness({ mode }: { mode?: "always" | "sticky-bottom" }) {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [isPinned, setIsPinned] = useState(false);
  const rect = useFloatPosition(anchorRef, isPinned, mode ? { mode } : undefined);
  const style = isPinned && rect
    ? { position: "fixed" as const, bottom: 16, left: rect.left, width: rect.width, zIndex: 30 }
    : undefined;
  return (
    <div ref={anchorRef}>
      <button type="button" data-testid="toggle" onClick={() => setIsPinned((v) => !v)}>
        toggle
      </button>
      <div data-testid="wrapper" style={style}>
        <Counter />
      </div>
    </div>
  );
}

describe("pin toggle keeps the editor mounted (Bug 1 regression)", () => {
  it("preserves child state across pin and unpin", () => {
    render(<Harness />);
    const counter = screen.getByTestId("counter");
    expect(counter).toHaveTextContent("n=0");

    act(() => counter.click());
    act(() => counter.click());
    expect(screen.getByTestId("counter")).toHaveTextContent("n=2");

    act(() => screen.getByTestId("toggle").click());
    expect(screen.getByTestId("counter")).toHaveTextContent("n=2");

    act(() => screen.getByTestId("toggle").click());
    expect(screen.getByTestId("counter")).toHaveTextContent("n=2");
  });
});

describe("sticky-bottom mode", () => {
  const originalGetRect = Element.prototype.getBoundingClientRect;
  const mockHeight = 60;
  const mockLeft = 80;
  const mockWidth = 400;

  afterEach(() => {
    Element.prototype.getBoundingClientRect = originalGetRect;
    vi.restoreAllMocks();
  });

  function mockAnchorRect(bottom: number) {
    Element.prototype.getBoundingClientRect = function () {
      // Only mock for the anchor div — the Counter button and others can
      // safely share the same numbers; only `bottom` vs `innerHeight` matters
      // for the hook's decision.
      return {
        x: mockLeft,
        y: bottom - mockHeight,
        top: bottom - mockHeight,
        left: mockLeft,
        right: mockLeft + mockWidth,
        bottom,
        width: mockWidth,
        height: mockHeight,
        toJSON() { return {}; },
      } as DOMRect;
    };
  }

  it("does not float while anchor.bottom is on or above the viewport bottom", () => {
    (window as unknown as { innerHeight: number }).innerHeight = 800;
    mockAnchorRect(500); // 500 <= 800 → in view → no float
    render(<Harness mode="sticky-bottom" />);

    act(() => screen.getByTestId("toggle").click()); // pin

    const wrapper = screen.getByTestId("wrapper");
    expect(wrapper.style.position).toBe("");
  });

  it("floats once anchor.bottom scrolls below the viewport bottom", () => {
    (window as unknown as { innerHeight: number }).innerHeight = 800;
    mockAnchorRect(900); // 900 > 800 → below viewport → should float
    render(<Harness mode="sticky-bottom" />);

    act(() => screen.getByTestId("toggle").click()); // pin

    const wrapper = screen.getByTestId("wrapper");
    expect(wrapper.style.position).toBe("fixed");
    expect(wrapper.style.bottom).toBe("16px");
    expect(wrapper.style.width).toBe(`${mockWidth}px`);
  });

  it("returns null again when the user scrolls back into view", () => {
    (window as unknown as { innerHeight: number }).innerHeight = 800;
    mockAnchorRect(900); // below
    render(<Harness mode="sticky-bottom" />);
    act(() => screen.getByTestId("toggle").click());

    expect(screen.getByTestId("wrapper").style.position).toBe("fixed");

    // Simulate the user scrolling back so the anchor is in view again.
    mockAnchorRect(500);
    act(() => window.dispatchEvent(new Event("scroll")));

    expect(screen.getByTestId("wrapper").style.position).toBe("");
  });
});
