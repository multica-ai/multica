import { useState } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { JUMP_FOLLOW_MS, useJumpToLatest } from "./use-jump-to-latest";

let rafCallbacks: FrameRequestCallback[] = [];
let now = 0;

function flushFrame(advanceMs = 16) {
  now += advanceMs;
  act(() => {
    const callbacks = rafCallbacks;
    rafCallbacks = [];
    callbacks.forEach((cb) => cb(performance.now()));
  });
}

// jsdom has no layout: give the scroller fixed metrics and clamp scrollTop
// like a browser would. `scrollHeight` is mutable so a test can grow the
// content mid-jump the way Virtuoso measuring real row heights does.
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
  const { visible, jump } = useJumpToLatest(el);
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
      {visible && <button onClick={jump}>latest</button>}
    </div>
  );
}

beforeEach(() => {
  rafCallbacks = [];
  now = 0;
  vi.spyOn(performance, "now").mockImplementation(() => now);
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

describe("useJumpToLatest", () => {
  it("shows only when the bottom is more than one viewport away", () => {
    render(<Harness metrics={{ scrollHeight: 2000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");
    expect(screen.getByRole("button", { name: "latest" })).toBeTruthy();

    scroller.scrollTop = 1100; // 400px from the bottom
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.queryByRole("button", { name: "latest" })).toBeNull();
  });

  it("hides once the reader has scrolled past the top fifth of the page", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");

    scroller.scrollTop = 800; // exactly 20% of the page
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.getByRole("button", { name: "latest" })).toBeTruthy();

    scroller.scrollTop = 900; // past 20%, though several screens are still below
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.queryByRole("button", { name: "latest" })).toBeNull();
  });

  it("stays hidden when the view opens restored past the top fifth", () => {
    render(<Harness metrics={{ scrollHeight: 4000, clientHeight: 500 }} />);
    const scroller = screen.getByTestId("scroller");
    scroller.scrollTop = 2000;
    fireEvent.scroll(scroller);
    flushFrame();
    expect(screen.queryByRole("button", { name: "latest" })).toBeNull();
  });

  it("stays hidden for a timeline shorter than two viewports", () => {
    render(<Harness metrics={{ scrollHeight: 900, clientHeight: 500 }} />);
    expect(screen.queryByRole("button", { name: "latest" })).toBeNull();
  });

  it("chases the bottom while content keeps growing", () => {
    const metrics = { scrollHeight: 2000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    expect(scroller.scrollTop).toBe(1500);

    metrics.scrollHeight = 2600; // tail rows measured taller than estimated
    flushFrame();
    expect(scroller.scrollTop).toBe(2100);
  });

  it("stops chasing once the reader scrolls", () => {
    const metrics = { scrollHeight: 2000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    fireEvent.wheel(scroller);
    scroller.scrollTop = 300;
    metrics.scrollHeight = 2600;
    flushFrame();
    flushFrame();
    expect(scroller.scrollTop).toBe(300);
  });

  it("keeps following when content grows after the bottom held still", () => {
    const metrics = { scrollHeight: 2000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    // Bottom holds for several frames, then a late tail image lands.
    for (let i = 0; i < 5; i++) flushFrame();
    expect(scroller.scrollTop).toBe(1500);
    metrics.scrollHeight = 3800;
    flushFrame(250);
    expect(scroller.scrollTop).toBe(3300);
  });

  it("stops following once the follow window ends", () => {
    const metrics = { scrollHeight: 2000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    flushFrame(JUMP_FOLLOW_MS);
    metrics.scrollHeight = 2600;
    flushFrame();
    flushFrame();
    expect(scroller.scrollTop).toBe(1500);
  });

  it("stops following on a key pressed outside the scroller", () => {
    const metrics = { scrollHeight: 2000, clientHeight: 500 };
    render(<Harness metrics={metrics} />);
    const scroller = screen.getByTestId("scroller");

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    // The button unmounts and focus falls back to <body>.
    fireEvent.keyDown(document.body, { key: "PageUp" });
    scroller.scrollTop = 600;
    metrics.scrollHeight = 2600;
    flushFrame();
    flushFrame();
    expect(scroller.scrollTop).toBe(600);
  });

  it("releases the document key listener when the follow ends", () => {
    const remove = vi.spyOn(document, "removeEventListener");
    render(<Harness metrics={{ scrollHeight: 2000, clientHeight: 500 }} />);

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    flushFrame(JUMP_FOLLOW_MS);
    expect(remove).toHaveBeenCalledWith("keydown", expect.any(Function), true);
  });
});
