import type { ReactNode } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  ScrollRestorationProvider,
  useRestoredScrollEntry,
  useRestoredScrollOffset,
  useRestoredScrollRef,
  useScrollRestorationAdapter,
  type ScrollRestorationAdapter,
} from "./scroll-restoration";

function makeAdapter(get: ScrollRestorationAdapter["get"]): ScrollRestorationAdapter {
  return { get };
}

describe("scroll-restoration public surface", () => {
  it("useScrollRestorationAdapter returns null without a provider", () => {
    const { result } = renderHook(() => useScrollRestorationAdapter());
    expect(result.current).toBeNull();
  });

  it("useRestoredScrollOffset remains a thin wrapper returning only top (back-compat)", () => {
    const adapter = makeAdapter(() => ({
      top: 123,
      height: 456,
      contentKey: "ck",
    }));
    const { result } = renderHook(() => useRestoredScrollOffset("main"), {
      wrapper: ({ children }) => (
        <ScrollRestorationProvider adapter={adapter}>
          {children}
        </ScrollRestorationProvider>
      ),
    });
    expect(result.current).toBe(123);
  });

  it("useRestoredScrollRef restores both axes; a sideways-only entry has no vertical offset", () => {
    const adapter = makeAdapter(() => ({ top: 0, height: 800, left: 640 }));
    const wrapper = ({ children }: { children: ReactNode }) => (
      <ScrollRestorationProvider adapter={adapter}>
        {children}
      </ScrollRestorationProvider>
    );
    const ref = renderHook(() => useRestoredScrollRef("board"), { wrapper });
    const offset = renderHook(() => useRestoredScrollOffset("board"), {
      wrapper,
    });

    const el = document.createElement("div");
    ref.result.current(el);

    expect(el.scrollLeft).toBe(640);
    expect(el.scrollTop).toBe(0);
    expect(offset.result.current).toBeUndefined();
  });

  it("all hooks return undefined without a provider (web no-op path)", () => {
    const entry = renderHook(() => useRestoredScrollEntry("x"));
    const offset = renderHook(() => useRestoredScrollOffset("x"));
    expect(entry.result.current).toBeUndefined();
    expect(offset.result.current).toBeUndefined();
  });
});

describe("useRestoredScrollRef while the content settles", () => {
  let frames = new Map<number, FrameRequestCallback>();
  let nextId = 0;
  let now = 0;

  beforeEach(() => {
    frames = new Map();
    now = 0;
    vi.stubGlobal("requestAnimationFrame", (cb: FrameRequestCallback) => {
      frames.set(++nextId, cb);
      return nextId;
    });
    vi.stubGlobal("cancelAnimationFrame", (id: number) => frames.delete(id));
    vi.spyOn(performance, "now").mockImplementation(() => now);
  });

  afterEach(() => {
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
    document.body.innerHTML = "";
  });

  function nextFrame(ms = 16) {
    now += ms;
    const pending = [...frames.values()];
    frames.clear();
    pending.forEach((cb) => cb(now));
  }

  // jsdom has no layout: model a scroller whose content height can change
  // and whose offset the "browser" clamps to it.
  function scroller(contentHeight: number) {
    const el = document.createElement("div");
    document.body.appendChild(el);
    const viewport = 600;
    let height = contentHeight;
    let top = 0;
    const clamp = (v: number) => Math.max(0, Math.min(v, height - viewport));
    Object.defineProperties(el, {
      scrollHeight: { get: () => height },
      scrollTop: { get: () => top, set: (v: number) => { top = clamp(v); } },
    });
    return {
      el,
      resize(next: number) {
        height = next;
        top = clamp(top);
      },
      // Content above the viewport changing size moves what is on screen.
      shiftAbove(delta: number) {
        height += delta;
        top = clamp(top + delta);
      },
    };
  }

  function attach(el: HTMLElement, top: number) {
    const adapter = makeAdapter(() => ({ top, height: 3000 }));
    const { result } = renderHook(() => useRestoredScrollRef("main"), {
      wrapper: ({ children }) => (
        <ScrollRestorationProvider adapter={adapter}>{children}</ScrollRestorationProvider>
      ),
    });
    result.current(el);
    return result.current;
  }

  it("reaches the saved offset once content that lands after mount makes room", () => {
    const view = scroller(600);
    attach(view.el, 1500);
    expect(view.el.scrollTop).toBe(0);

    view.resize(3000);
    nextFrame();
    expect(view.el.scrollTop).toBe(1500);
  });

  it("holds the offset through a layout shift above it", () => {
    const view = scroller(3000);
    attach(view.el, 1500);
    expect(view.el.scrollTop).toBe(1500);

    view.shiftAbove(-60);
    nextFrame();
    expect(view.el.scrollTop).toBe(1500);
  });

  it("yields to the user scrolling before the content lands", () => {
    const view = scroller(600);
    attach(view.el, 1500);

    view.el.dispatchEvent(new Event("wheel"));
    view.resize(3000);
    nextFrame();
    expect(view.el.scrollTop).toBe(0);
  });

  it("yields to other code moving the offset without the content changing", () => {
    const view = scroller(3000);
    attach(view.el, 1500);

    view.el.scrollTop = 400;
    nextFrame();
    view.resize(3200);
    nextFrame();
    expect(view.el.scrollTop).toBe(400);
  });

  it("lets go after the settle window", () => {
    const view = scroller(600);
    attach(view.el, 1500);

    nextFrame(2000);
    view.resize(3000);
    nextFrame();
    expect(view.el.scrollTop).toBe(0);
  });

  it("skips pixel restore when disabled while still registering capture", () => {
    const view = scroller(3000);
    const registerScrollRoot = vi.fn(() => vi.fn());
    const get = vi.fn(() => ({ top: 1500, height: 3000 }));
    const adapter: ScrollRestorationAdapter = { get, registerScrollRoot };
    const { result } = renderHook(
      () => useRestoredScrollRef("main", { disabled: true }),
      {
        wrapper: ({ children }) => (
          <ScrollRestorationProvider adapter={adapter}>{children}</ScrollRestorationProvider>
        ),
      },
    );

    result.current(view.el);
    expect(get).not.toHaveBeenCalled();
    expect(registerScrollRoot).toHaveBeenCalledWith("main", view.el);
    expect(view.el.scrollTop).toBe(0);

    view.resize(3400);
    nextFrame();
    expect(view.el.scrollTop).toBe(0);
  });

  it("cancels an active settle hold if disabled flips true after attach", () => {
    const view = scroller(3000);
    const adapter = makeAdapter(() => ({ top: 1500, height: 3000 }));
    const { result, rerender } = renderHook(
      ({ disabled }: { disabled: boolean }) =>
        useRestoredScrollRef("main", { disabled }),
      {
        initialProps: { disabled: false },
        wrapper: ({ children }) => (
          <ScrollRestorationProvider adapter={adapter}>{children}</ScrollRestorationProvider>
        ),
      },
    );
    const refBefore = result.current;
    result.current(view.el);
    expect(view.el.scrollTop).toBe(1500);

    rerender({ disabled: true });
    expect(result.current).toBe(refBefore);

    view.el.scrollTop = 400;
    view.resize(3400);
    nextFrame();
    expect(view.el.scrollTop).toBe(400);
  });
});
