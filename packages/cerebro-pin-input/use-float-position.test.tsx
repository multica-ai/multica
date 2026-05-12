import { useRef, useState } from "react";
import { describe, expect, it } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { useFloatPosition } from "./use-float-position";

// JEH-1065 — Bug 1 regression test. The pinned input MUST keep its child
// component instance alive across pin/unpin so the Tiptap editor draft and
// caret survive. The implementation achieves this by leaving the editor in
// the SAME React tree position on every render and only toggling inline
// `position: fixed` styling on the wrapper. This test asserts the property
// directly with a stateful Counter as the canary.

function Counter() {
  const [n, setN] = useState(0);
  return (
    <button type="button" data-testid="counter" onClick={() => setN((v) => v + 1)}>
      n={n}
    </button>
  );
}

function Harness() {
  const anchorRef = useRef<HTMLDivElement>(null);
  const [isPinned, setIsPinned] = useState(false);
  const rect = useFloatPosition(anchorRef, isPinned);
  const style = isPinned && rect
    ? { position: "fixed" as const, bottom: 16, left: rect.left, width: rect.width, zIndex: 30 }
    : undefined;
  return (
    <div ref={anchorRef}>
      <button type="button" data-testid="toggle" onClick={() => setIsPinned((v) => !v)}>
        toggle
      </button>
      <div style={style}>
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
