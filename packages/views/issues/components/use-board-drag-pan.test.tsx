/**
 * @vitest-environment jsdom
 */
import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup, fireEvent } from "@testing-library/react";
import { useBoardDragPan } from "./use-board-drag-pan";

function Harness() {
  const pan = useBoardDragPan<HTMLDivElement>();
  return (
    <div
      data-testid="scroller"
      ref={pan.ref}
      onPointerDown={pan.onPointerDown}
      onPointerMove={pan.onPointerMove}
      onPointerUp={pan.onPointerUp}
      onPointerCancel={pan.onPointerCancel}
      onLostPointerCapture={pan.onLostPointerCapture}
    >
      <div data-testid="blank" style={{ width: 10, height: 10 }} />
      <div data-board-card="" data-testid="card">
        <span data-testid="card-child" />
      </div>
      <button data-testid="ctrl-button">b</button>
      <label data-testid="ctrl-label">l</label>
      <div role="link" data-testid="ctrl-rolelink">r</div>
    </div>
  );
}

// jsdom lacks pointer-capture methods and doesn't lay out; stub capture and a
// scrollable range so the hook's clamp has room to move. Records capture calls.
function stubCapture(el: HTMLElement, scrollWidth = 1000, clientWidth = 300) {
  const captured: number[] = [];
  const released: number[] = [];
  el.setPointerCapture = (id: number) => void captured.push(id);
  el.releasePointerCapture = (id: number) => void released.push(id);
  Object.defineProperty(el, "scrollWidth", { value: scrollWidth, configurable: true });
  Object.defineProperty(el, "clientWidth", { value: clientWidth, configurable: true });
  return { captured, released };
}

function down(target: Element, x: number, button = 0, pointerType = "mouse") {
  fireEvent.pointerDown(target, {
    button,
    clientX: x,
    clientY: 50,
    pointerId: 1,
    pointerType,
  });
}
// `buttons: 1` = left button held — the real browser value during a left drag.
function move(target: Element, x: number, buttons = 1) {
  fireEvent.pointerMove(target, {
    clientX: x,
    clientY: 50,
    pointerId: 1,
    pointerType: "mouse",
    buttons,
  });
}
function up(target: Element) {
  fireEvent.pointerUp(target, {
    clientX: 0,
    clientY: 50,
    pointerId: 1,
    pointerType: "mouse",
  });
}

describe("useBoardDragPan", () => {
  afterEach(cleanup);

  it("pans horizontally on a left drag from blank background (real event order)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 100;

    // pointerdown → move past 5px threshold → follow cursor → up
    down(getByTestId("blank"), 200);
    move(el, 208); // 8px > threshold: activates; delta from last (200) = 8
    expect(el.scrollLeft).toBe(92); // 100 - 8
    move(el, 188); // moved right 20 -> scroll left
    expect(el.scrollLeft).toBe(112);

    up(el);
    move(el, 400); // after release: ignored
    expect(el.scrollLeft).toBe(112);
  });

  it("does not activate before the ~5px threshold (plain click)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 50;

    down(getByTestId("blank"), 200);
    move(el, 203); // 3px < 5px threshold
    expect(el.scrollLeft).toBe(50);
  });

  it("ignores drags that start on a card so only dnd-kit runs (P2-5 invariant)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    const { captured } = stubCapture(el);
    el.scrollLeft = 30;

    down(getByTestId("card-child"), 200); // starts inside [data-board-card]
    move(el, 260);
    expect(el.scrollLeft).toBe(30); // never panned, card sorting untouched
    // The pan hook must not claim the pointer for a card-origin gesture, or it
    // would steal the drag from dnd-kit.
    expect(captured).toEqual([]);
    expect(el.style.userSelect).toBe(""); // no selection suppression either
  });

  it("ignores drags that start on interactive controls (label, role=link, button)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);

    for (const id of ["ctrl-button", "ctrl-label", "ctrl-rolelink"]) {
      el.scrollLeft = 30;
      down(getByTestId(id), 200);
      move(el, 260);
      expect(el.scrollLeft).toBe(30);
    }
  });

  it("ignores the right button entirely (context menu path untouched)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 40;

    down(getByTestId("blank"), 200, 2); // right button
    move(el, 260, 2);
    expect(el.scrollLeft).toBe(40);
  });

  it("does not stay stuck when the release event is lost", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 100;

    down(getByTestId("blank"), 200);
    move(el, 220); // activates, pans
    expect(el.scrollLeft).toBe(80);

    // Release happened off-window: no pointerup fired. Next move reports the
    // button no longer held (buttons === 0) and must NOT keep panning.
    move(el, 400, 0);
    expect(el.scrollLeft).toBe(80);
    move(el, 500, 0);
    expect(el.scrollLeft).toBe(80);
  });

  it("only reads the horizontal axis (scrollTop untouched)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 50;
    const topBefore = el.scrollTop;

    down(getByTestId("blank"), 200);
    move(el, 220); // activate
    fireEvent.pointerMove(el, {
      clientX: 220,
      clientY: 400,
      pointerId: 1,
      pointerType: "mouse",
      buttons: 1,
    });
    expect(el.scrollTop).toBe(topBefore);
  });

  it("clamps at the left edge without bouncing back (scrollLeft never goes negative)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 10;

    down(getByTestId("blank"), 100);
    move(el, 110); // activate; delta 10 -> would be 0
    expect(el.scrollLeft).toBe(0);
    // Keep dragging in the same direction well past the edge: stays at 0, no
    // negative overscroll that would spring back on the next frame.
    move(el, 300);
    expect(el.scrollLeft).toBe(0);
    move(el, 500);
    expect(el.scrollLeft).toBe(0);
  });

  it("clamps at the right edge (never exceeds scrollWidth - clientWidth)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller"); // max = 1000 - 300 = 700
    stubCapture(el);
    el.scrollLeft = 690;

    down(getByTestId("blank"), 300);
    move(el, 290); // drag left -> content moves right, scrollLeft += 10 -> 700
    expect(el.scrollLeft).toBe(700);
    move(el, 100); // further past the edge stays clamped
    expect(el.scrollLeft).toBe(700);
  });

  it("suppresses text selection from pointerdown (P1-3) and restores it on release", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 100;

    down(getByTestId("blank"), 200);
    // Suppressed immediately on down — NOT deferred to the 5px threshold.
    expect(el.style.userSelect).toBe("none");
    move(el, 220); // activate
    expect(el.style.userSelect).toBe("none");

    up(el);
    expect(el.style.userSelect).toBe(""); // restored on release
  });

  it("vetoes selectstart / dragstart while a gesture is pending (P1-3)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);

    // `selectstart` isn't in fireEvent's map; dispatch a real cancelable event
    // and check whether the handler called preventDefault (dispatchEvent
    // returns false when it did).
    const fireSelectStart = () =>
      el.dispatchEvent(new Event("selectstart", { bubbles: true, cancelable: true }));
    const fireDragStart = () =>
      el.dispatchEvent(new Event("dragstart", { bubbles: true, cancelable: true }));

    // No gesture yet: events pass through untouched.
    expect(fireSelectStart()).toBe(true);
    expect(fireDragStart()).toBe(true);

    down(getByTestId("blank"), 200);
    // Gesture pending: both are prevented (dispatchEvent returns false when
    // preventDefault was called).
    expect(fireSelectStart()).toBe(false);
    expect(fireDragStart()).toBe(false);

    up(el);
    expect(fireSelectStart()).toBe(true); // released after gesture end
  });

  it("does not set touch-action because touch/pen stay browser-owned", () => {
    const { getByTestId } = render(<Harness />);
    expect(getByTestId("scroller").style.touchAction).toBe("");
  });

  it("ignores touch and pen pointerdown entirely (mouse-only scope)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    const { captured } = stubCapture(el);
    el.scrollLeft = 100;

    const touchDown = fireEvent.pointerDown(getByTestId("blank"), {
      button: 0,
      clientX: 200,
      clientY: 50,
      pointerId: 11,
      pointerType: "touch",
    });
    expect(touchDown).toBe(true);
    expect(captured).toEqual([]);
    expect(el.style.userSelect).toBe("");
    fireEvent.pointerMove(el, {
      clientX: 260,
      clientY: 50,
      pointerId: 11,
      pointerType: "touch",
      buttons: 1,
    });
    expect(el.scrollLeft).toBe(100);

    const penDown = fireEvent.pointerDown(getByTestId("blank"), {
      button: 0,
      clientX: 200,
      clientY: 50,
      pointerId: 12,
      pointerType: "pen",
    });
    expect(penDown).toBe(true);
    expect(captured).toEqual([]);
    expect(el.style.userSelect).toBe("");
    fireEvent.pointerMove(el, {
      clientX: 260,
      clientY: 50,
      pointerId: 12,
      pointerType: "pen",
      buttons: 1,
    });
    expect(el.scrollLeft).toBe(100);
  });

  it("captures the pointer on pointerdown (before any move) so exit can't lose it", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    const { captured, released } = stubCapture(el);

    down(getByTestId("blank"), 200);
    expect(captured).toEqual([1]); // captured immediately, not deferred to move

    up(el);
    expect(released).toEqual([1]); // released on gesture end
  });

  it("keeps panning after the pointer leaves the container (no scroll-back on exit)", () => {
    const { getByTestId } = render(<Harness />);
    const el = getByTestId("scroller");
    stubCapture(el);
    el.scrollLeft = 300;

    down(getByTestId("blank"), 200);
    move(el, 220); // activate; delta 20 -> 280

    // Pointer moves outside the container bounds. Because it's captured on
    // pointerdown, the events still target `el` and we keep following clientX.
    // Nothing pulls scrollLeft back toward the start.
    fireEvent.pointerMove(el, {
      clientX: 260,
      clientY: 5000,
      pointerId: 1,
      pointerType: "mouse",
      buttons: 1,
    });
    expect(el.scrollLeft).toBe(240); // 280 - 40, monotonic follow, no bounce
    fireEvent.pointerMove(el, {
      clientX: 300,
      clientY: 5000,
      pointerId: 1,
      pointerType: "mouse",
      buttons: 1,
    });
    expect(el.scrollLeft).toBe(200); // 240 - 40, still following

    up(el);
    expect(el.scrollLeft).toBe(200); // stays put after release
  });
});
