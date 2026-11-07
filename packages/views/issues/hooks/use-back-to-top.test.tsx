import { useState } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useBackToTop } from "./use-back-to-top";

let rafCallbacks: FrameRequestCallback[] = [];

function flushFrame() {
  act(() => {
    const callbacks = rafCallbacks;
    rafCallbacks = [];
    callbacks.forEach((cb) => cb(0));
  });
}

// jsdom has no layout: give the scroller fixed metrics and clamp scrollTop
// like a browser would (mirrors use-jump-to-latest.test).
function defineMetrics(el: HTMLElement, metrics: { scrollHeight: number; clientHeight: number }) {
  let top = 0;
  Object.defineProperty(el, "clientHeight", { configurable: true, get: () => metrics.clientHeight });
  Object.defineProperty(el, "scrollHeight", { configurable: true, get: () => metrics.scrollHeight });
  Object.defineProperty(el, "scrollTop", {
    configurable: true,
    get: () => top,
    set: (v: number) => {
      top = Math.max(0, Math.min(v, metrics.scrollHeight - metrics.clientHeight));
    },
  });
}

function Harness({ metrics }: { metrics: { scrollHeight: number; clientHeight: number } }) {
  const [el, setEl] = useState<HTMLDivElement | null>(null);
  const { visible, toTop } = useBackToTop(el);
  return (
    <div
      data-testid="scroller"
      ref={(node) => {
        if (node && node !== el) {
          defineMetrics(node, metrics);
          setEl(node);
        }
      }}
    >
      {visible && <button onClick={toTop}>top</button>}
    </div>
  );
}

beforeEach(() => {
  rafCallbacks = [];
  vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
    rafCallbacks.push(cb);
    return rafCallbacks.length;
  });
  vi.stubGlobal("cancelAnimationFrame", () => {});
});

afterEach(() => {
  vi.unstubAllGlobals();
  vi.restoreAllMocks();
});

describe("useBackToTop", () => {
  it("shows only once the reader is more than one viewport below the top", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");
    expect(screen.queryByRole("button", { name: "top" })).toBeNull();

    scroller.scrollTop = 500; // exactly one viewport — still beside the top
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.queryByRole("button", { name: "top" })).toBeNull();

    scroller.scrollTop = 501;
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.getByRole("button", { name: "top" })).toBeTruthy();
  });

  it("stays visible all the way at the bottom of the page", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");

    scroller.scrollTop = 3500;
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.getByRole("button", { name: "top" })).toBeTruthy();
  });

  it("appears when the view opens restored deep in the timeline", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");
    scroller.scrollTop = 2000;
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.getByRole("button", { name: "top" })).toBeTruthy();
  });

  it("stays hidden for a timeline shorter than two viewports", () => {
    render(<Harness metrics={{ scrollHeight: 900, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");

    scroller.scrollTop = 400; // the whole scrollable extent
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.queryByRole("button", { name: "top" })).toBeNull();
  });

  it("scrolls to the top on activation and stays there while late content lands", () => {
    const metrics = { scrollHeight: 4000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");
    scroller.scrollTop = 2000;
    fireEvent.scroll(scroller);
    flushFrame();

    fireEvent.click(screen.getByRole("button", { name: "top" }));
    expect(scroller.scrollTop).toBe(0);

    // The jump's scrollTop write fires a scroll event in the browser;
    // jsdom needs it supplied, and the button leaves with it.
    fireEvent.scroll(scroller);
    flushFrame();

    // Head rows measured taller than estimated — the top must not move.
    metrics.scrollHeight = 4600;
    flushFrame();
    flushFrame();
    expect(scroller.scrollTop).toBe(0);
    expect(screen.queryByRole("button", { name: "top" })).toBeNull();
  });

  it("leaves the reader's scroll alone until they jump", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");

    scroller.scrollTop = 1200;
    fireEvent.scroll(scroller);
    flushFrame();
    expect(scroller.scrollTop).toBe(1200);
  });
});
