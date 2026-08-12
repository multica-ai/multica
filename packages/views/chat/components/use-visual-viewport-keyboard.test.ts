import { describe, it, expect, beforeEach, afterEach } from "vitest";
import { act, renderHook } from "@testing-library/react";
import { useVisualViewportKeyboard } from "./use-visual-viewport-keyboard";

class FakeVisualViewport extends EventTarget {
  height: number;
  offsetTop = 0;
  scale = 1;
  constructor(height: number) {
    super();
    this.height = height;
  }
  resize(next: Partial<Pick<FakeVisualViewport, "height" | "offsetTop" | "scale">>) {
    Object.assign(this, next);
    this.dispatchEvent(new Event("resize"));
  }
}

describe("useVisualViewportKeyboard", () => {
  let vv: FakeVisualViewport;

  beforeEach(() => {
    // iOS Safari shrinks window.innerHeight together with the visual
    // viewport when the keyboard opens, so the hook must key off the stable
    // documentElement.clientHeight. Model that worst case: innerHeight
    // always mirrors the visual viewport height.
    Object.defineProperty(document.documentElement, "clientHeight", {
      configurable: true,
      value: 800,
    });
    vv = new FakeVisualViewport(800);
    Object.defineProperty(window, "innerHeight", {
      configurable: true,
      get: () => vv.height,
    });
    Object.defineProperty(window, "visualViewport", {
      configurable: true,
      value: vv,
    });
  });

  afterEach(() => {
    Reflect.deleteProperty(document.documentElement, "clientHeight");
    Reflect.deleteProperty(window, "innerHeight");
    Reflect.deleteProperty(window, "visualViewport");
  });

  it("reports null while the visual viewport matches the layout viewport", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    expect(result.current).toBeNull();
  });

  it("ignores small browser-chrome height changes below the keyboard threshold", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 740 }));
    expect(result.current).toBeNull();
  });

  it("reports geometry when the keyboard shrinks the visual viewport", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 500 }));
    expect(result.current).toEqual({ occludedBottom: 300, viewportHeight: 500 });
  });

  it("keeps viewportHeight while a pan moves the occluded bottom", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 500, offsetTop: 120 }));
    expect(result.current).toEqual({ occludedBottom: 180, viewportHeight: 500 });
  });

  it("still reports the keyboard after a full iOS pan (occludedBottom 0)", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 500, offsetTop: 300 }));
    expect(result.current).toEqual({ occludedBottom: 0, viewportHeight: 500 });
  });

  it("returns to null when the keyboard closes", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 500 }));
    act(() => vv.resize({ height: 800, offsetTop: 0 }));
    expect(result.current).toBeNull();
  });

  it("ignores pinch-zoom (scale != 1)", () => {
    const { result } = renderHook(() => useVisualViewportKeyboard());
    act(() => vv.resize({ height: 400, scale: 2 }));
    expect(result.current).toBeNull();
  });

  it("supports browsers without visualViewport", () => {
    Reflect.deleteProperty(window, "visualViewport");
    const { result } = renderHook(() => useVisualViewportKeyboard());
    expect(result.current).toBeNull();
  });
});
