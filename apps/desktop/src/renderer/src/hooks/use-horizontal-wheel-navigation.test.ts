import { afterEach, describe, expect, it } from "vitest";
import {
  createHorizontalWheelNavigationTracker,
  isPageNavigationWheelCandidate,
  wheelGestureFromDeltaX,
} from "./use-horizontal-wheel-navigation";

function makeSurface() {
  const surface = document.createElement("div");
  surface.setAttribute("data-page-navigation-surface", "");
  document.body.append(surface);
  return surface;
}

function dispatchWheel(
  target: Element,
  init: WheelEventInit,
  tracker = createHorizontalWheelNavigationTracker({
    triggerThreshold: 90,
    debounceMs: 0,
  }),
) {
  let gesture: "back" | "forward" | null = null;
  target.addEventListener(
    "wheel",
    (event) => {
      gesture = tracker.handleWheel(event as WheelEvent);
    },
    { once: true },
  );
  target.dispatchEvent(
    new WheelEvent("wheel", {
      bubbles: true,
      cancelable: true,
      ...init,
    }),
  );
  return gesture;
}

function setHorizontalScrollMetrics(
  element: HTMLElement,
  metrics: { clientWidth: number; scrollWidth: number; scrollLeft: number },
) {
  Object.defineProperties(element, {
    clientWidth: { configurable: true, value: metrics.clientWidth },
    scrollWidth: { configurable: true, value: metrics.scrollWidth },
    scrollLeft: {
      configurable: true,
      value: metrics.scrollLeft,
      writable: true,
    },
  });
}

afterEach(() => {
  document.body.replaceChildren();
});

describe("wheelGestureFromDeltaX", () => {
  it("maps browser-style horizontal wheel deltas to history direction", () => {
    expect(wheelGestureFromDeltaX(-1)).toBe("back");
    expect(wheelGestureFromDeltaX(1)).toBe("forward");
    expect(wheelGestureFromDeltaX(0)).toBeNull();
  });
});

describe("isPageNavigationWheelCandidate", () => {
  it("requires a dominant horizontal wheel gesture inside the page surface", () => {
    const surface = makeSurface();

    let seen: boolean | null = null;
    surface.addEventListener(
      "wheel",
      (event) => {
        seen = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );

    surface.dispatchEvent(
      new WheelEvent("wheel", {
        bubbles: true,
        deltaX: -20,
        deltaY: -4,
      }),
    );

    expect(seen).toBe(true);
  });

  it("ignores vertical wheels, modifier wheels, and events outside the page surface", () => {
    const surface = makeSurface();
    const outside = document.createElement("div");
    document.body.append(outside);

    let vertical: boolean | null = null;
    surface.addEventListener(
      "wheel",
      (event) => {
        vertical = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    surface.dispatchEvent(
      new WheelEvent("wheel", { bubbles: true, deltaX: -8, deltaY: -30 }),
    );

    let modified: boolean | null = null;
    surface.addEventListener(
      "wheel",
      (event) => {
        modified = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    surface.dispatchEvent(
      new WheelEvent("wheel", {
        bubbles: true,
        deltaX: -30,
        deltaY: 0,
        shiftKey: true,
      }),
    );

    let outOfSurface: boolean | null = null;
    outside.addEventListener(
      "wheel",
      (event) => {
        outOfSurface = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    outside.dispatchEvent(
      new WheelEvent("wheel", { bubbles: true, deltaX: -30, deltaY: 0 }),
    );

    expect(vertical).toBe(false);
    expect(modified).toBe(false);
    expect(outOfSurface).toBe(false);
  });

  it("lets local horizontal scrollers consume wheels before page navigation", () => {
    const surface = makeSurface();
    const scroller = document.createElement("div");
    const child = document.createElement("div");
    scroller.append(child);
    surface.append(scroller);

    setHorizontalScrollMetrics(scroller, {
      clientWidth: 100,
      scrollWidth: 300,
      scrollLeft: 50,
    });

    let candidate: boolean | null = null;
    child.addEventListener(
      "wheel",
      (event) => {
        candidate = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    child.dispatchEvent(
      new WheelEvent("wheel", { bubbles: true, deltaX: -30, deltaY: 0 }),
    );

    expect(candidate).toBe(false);
  });

  it("allows page navigation once a local horizontal scroller is at the edge", () => {
    const surface = makeSurface();
    const scroller = document.createElement("div");
    const child = document.createElement("div");
    scroller.append(child);
    surface.append(scroller);

    setHorizontalScrollMetrics(scroller, {
      clientWidth: 100,
      scrollWidth: 300,
      scrollLeft: 0,
    });

    let candidate: boolean | null = null;
    child.addEventListener(
      "wheel",
      (event) => {
        candidate = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    child.dispatchEvent(
      new WheelEvent("wheel", { bubbles: true, deltaX: -30, deltaY: 0 }),
    );

    expect(candidate).toBe(true);
  });

  it("ignores editable targets", () => {
    const surface = makeSurface();
    const input = document.createElement("input");
    surface.append(input);

    let candidate: boolean | null = null;
    input.addEventListener(
      "wheel",
      (event) => {
        candidate = isPageNavigationWheelCandidate(event);
      },
      { once: true },
    );
    input.dispatchEvent(
      new WheelEvent("wheel", { bubbles: true, deltaX: -30, deltaY: 0 }),
    );

    expect(candidate).toBe(false);
  });
});

describe("createHorizontalWheelNavigationTracker", () => {
  it("accumulates horizontal wheel movement before triggering", () => {
    const surface = makeSurface();
    const tracker = createHorizontalWheelNavigationTracker({
      triggerThreshold: 90,
      debounceMs: 0,
    });

    expect(dispatchWheel(surface, { deltaX: -45, deltaY: 0 }, tracker)).toBeNull();
    expect(dispatchWheel(surface, { deltaX: -50, deltaY: 0 }, tracker)).toBe("back");
  });

  it("resets accumulation when the horizontal direction changes", () => {
    const surface = makeSurface();
    const tracker = createHorizontalWheelNavigationTracker({
      triggerThreshold: 90,
      debounceMs: 0,
    });

    expect(dispatchWheel(surface, { deltaX: -60, deltaY: 0 }, tracker)).toBeNull();
    expect(dispatchWheel(surface, { deltaX: 60, deltaY: 0 }, tracker)).toBeNull();
    expect(dispatchWheel(surface, { deltaX: 35, deltaY: 0 }, tracker)).toBe("forward");
  });
});
