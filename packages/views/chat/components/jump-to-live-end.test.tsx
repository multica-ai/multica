import { useState } from "react";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { JUMP_MAX_FRAMES, useJumpToLiveEnd } from "./jump-to-live-end";

let rafCallbacks: FrameRequestCallback[] = [];

function flushFrame() {
  act(() => {
    const callbacks = rafCallbacks;
    rafCallbacks = [];
    callbacks.forEach((cb) => cb(0));
  });
}

type Metrics = { scrollHeight: number; clientHeight: number };

// jsdom has no layout: fixed metrics, with scrollTop clamped like a browser.
function defineMetrics(el: HTMLElement, metrics: Metrics) {
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

function Harness({
  metrics,
  pin,
  tailKey = "t1",
  tailIsReply = true,
}: {
  metrics: Metrics;
  pin: (el: HTMLElement) => void;
  tailKey?: string;
  tailIsReply?: boolean;
}) {
  const [el, setEl] = useState<HTMLDivElement | null>(null);
  const { visible, hasNewReply, jump } = useJumpToLiveEnd(el, () => el && pin(el), {
    enabled: true,
    tailKey,
    tailIsReply,
  });
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
      {visible && <button onClick={jump}>{hasNewReply ? "new reply" : "latest"}</button>}
    </div>
  );
}

function scrollTo(scroller: HTMLElement, top: number) {
  scroller.scrollTop = top;
  fireEvent.scroll(scroller);
  flushFrame();
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

const toBottom = (el: HTMLElement) => {
  el.scrollTop = el.scrollHeight - el.clientHeight;
};

describe("useJumpToLiveEnd", () => {
  it("shows only when the reader is more than a screen above the live end", () => {
    render(<Harness metrics={{ scrollHeight: 3000, clientHeight: 500 }} pin={toBottom} />);
    const scroller = screen.getByTestId("scroller");
    scrollTo(scroller, 2500);
    expect(screen.queryByRole("button")).toBeNull();

    scrollTo(scroller, 2100); // 400px up: still within a screen
    expect(screen.queryByRole("button")).toBeNull();

    scrollTo(scroller, 1000);
    expect(screen.getByRole("button", { name: "latest" })).toBeTruthy();
  });

  it("flags a reply that lands while the reader is away, and drops it back at the end", () => {
    const metrics = { scrollHeight: 3000, clientHeight: 500 };
    const { rerender } = render(<Harness metrics={metrics} pin={toBottom} tailKey="t1" />);
    const scroller = screen.getByTestId("scroller");
    scrollTo(scroller, 1000);

    rerender(<Harness metrics={metrics} pin={toBottom} tailKey="t2" />);
    expect(screen.getByRole("button", { name: "new reply" })).toBeTruthy();

    scrollTo(scroller, 2500);
    scrollTo(scroller, 1000);
    expect(screen.getByRole("button", { name: "latest" })).toBeTruthy();
  });

  it("does not flag the reader's own message", () => {
    const metrics = { scrollHeight: 3000, clientHeight: 500 };
    const { rerender } = render(<Harness metrics={metrics} pin={toBottom} tailKey="t1" />);
    const scroller = screen.getByTestId("scroller");
    scrollTo(scroller, 1000);

    rerender(<Harness metrics={metrics} pin={toBottom} tailKey="u1" tailIsReply={false} />);
    expect(screen.getByRole("button", { name: "latest" })).toBeTruthy();
  });

  it("re-aims until the list holds at the live end", () => {
    const metrics = { scrollHeight: 3000, clientHeight: 500 };
    const pin = vi.fn((el: HTMLElement) => {
      toBottom(el);
      // Virtuoso measures the rows it lands on: the first aim falls short.
      if (pin.mock.calls.length === 1) metrics.scrollHeight = 4000;
    });
    render(<Harness metrics={metrics} pin={pin} />);
    const scroller = screen.getByTestId("scroller");
    scrollTo(scroller, 0);

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    expect(scroller.scrollTop).toBe(2500);
    flushFrame();
    expect(scroller.scrollTop).toBe(3500);
    flushFrame();
    flushFrame();
    expect(pin).toHaveBeenCalledTimes(2);
  });

  it("gives up after a bounded number of frames", () => {
    const pin = vi.fn();
    render(<Harness metrics={{ scrollHeight: 3000, clientHeight: 500 }} pin={pin} />);
    scrollTo(screen.getByTestId("scroller"), 0);

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    for (let i = 0; i < JUMP_MAX_FRAMES + 5; i++) flushFrame();
    expect(pin.mock.calls.length).toBeLessThanOrEqual(JUMP_MAX_FRAMES);
  });

  it("stops re-aiming once the reader scrolls", () => {
    const pin = vi.fn();
    render(<Harness metrics={{ scrollHeight: 3000, clientHeight: 500 }} pin={pin} />);
    const scroller = screen.getByTestId("scroller");
    scrollTo(scroller, 0);

    fireEvent.click(screen.getByRole("button", { name: "latest" }));
    fireEvent.wheel(scroller);
    flushFrame();
    flushFrame();
    expect(pin).toHaveBeenCalledTimes(1);
  });
});
