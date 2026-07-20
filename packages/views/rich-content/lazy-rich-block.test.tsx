/**
 * Near-viewport lazy shell (MUL-4922 performance contract).
 *
 * jsdom has no IntersectionObserver, so these tests install a controllable
 * fake: it records observed elements and lets a test decide when a block
 * becomes near-viewport. That makes the deferral, the latch and the size
 * reservation observable rather than assumed.
 */
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, render, screen } from "@testing-library/react";
import { LazyRichBlock } from "./lazy-rich-block";

type IOCallback = (entries: { isIntersecting: boolean }[]) => void;

interface FakeObserver {
  callback: IOCallback;
  options?: IntersectionObserverInit;
  observed: Element[];
  disconnected: boolean;
}

let observers: FakeObserver[] = [];

beforeEach(() => {
  observers = [];
  class FakeIntersectionObserver {
    private readonly self: FakeObserver;
    constructor(callback: IOCallback, options?: IntersectionObserverInit) {
      this.self = { callback, options, observed: [], disconnected: false };
      observers.push(this.self);
    }
    observe(el: Element) {
      this.self.observed.push(el);
    }
    disconnect() {
      this.self.disconnected = true;
    }
    unobserve() {}
    takeRecords() {
      return [];
    }
  }
  vi.stubGlobal("IntersectionObserver", FakeIntersectionObserver);
});

afterEach(() => {
  vi.unstubAllGlobals();
});

/** Fire the near-viewport signal on every live observer. */
function enterViewport() {
  act(() => {
    for (const o of observers) o.callback([{ isIntersecting: true }]);
  });
}

const Expensive = () => <div data-testid="expensive">diagram</div>;

describe("LazyRichBlock", () => {
  it("does not mount its child until the block is near the viewport", () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    expect(screen.queryByTestId("expensive")).toBeNull();
    expect(observers).toHaveLength(1);
    expect(observers[0]?.observed).toHaveLength(1);
  });

  it("mounts the child once it becomes near-viewport", () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );
    expect(screen.queryByTestId("expensive")).toBeNull();

    enterViewport();

    expect(screen.getByTestId("expensive")).toBeInTheDocument();
  });

  // The latch: re-running Mermaid / rebuilding an iframe on every scroll pass
  // would be worse than mounting eagerly once.
  it("stops observing after mounting so the block never unmounts", () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );
    enterViewport();

    expect(observers[0]?.disconnected).toBe(true);

    // A later "left the viewport" signal must not tear the block down.
    act(() => {
      observers[0]?.callback([{ isIntersecting: false }]);
    });
    expect(screen.getByTestId("expensive")).toBeInTheDocument();
  });

  // Howard's stated risk: lazy mounting must not disturb Virtuoso's height
  // measurement. The shell therefore reserves the same space before and after.
  it("reserves the same height before and after mount", () => {
    const { container } = render(
      <LazyRichBlock reservedHeightPx={480}>
        <Expensive />
      </LazyRichBlock>,
    );
    const shell = container.querySelector("[data-rich-block-shell]") as HTMLElement;

    expect(shell.style.minHeight).toBe("480px");
    expect(shell.hasAttribute("data-mounted")).toBe(false);

    enterViewport();

    expect(shell.style.minHeight).toBe("480px");
    expect(shell.hasAttribute("data-mounted")).toBe(true);
  });

  it("watches an area larger than the viewport so blocks are ready on arrival", () => {
    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    const margin = observers[0]?.options?.rootMargin ?? "";
    const topPx = Number.parseInt(margin.split(" ")[0] ?? "0", 10);
    // Must exceed the chat list's own overscan (600px bottom) or a block would
    // still be blank when Virtuoso has already rendered its row.
    expect(topPx).toBeGreaterThan(600);
  });

  it("mounts immediately when IntersectionObserver is unavailable", () => {
    vi.unstubAllGlobals();
    vi.stubGlobal("IntersectionObserver", undefined);

    render(
      <LazyRichBlock reservedHeightPx={280}>
        <Expensive />
      </LazyRichBlock>,
    );

    // Degrading to eager mount is correct; rendering nothing would not be.
    expect(screen.getByTestId("expensive")).toBeInTheDocument();
  });
});
