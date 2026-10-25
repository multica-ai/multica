// @vitest-environment jsdom
import { StrictMode, createElement } from "react";
import { act, cleanup, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { useRelativeTimeTick } from "./use-relative-time-tick";

beforeEach(() => {
  vi.useFakeTimers();
  vi.spyOn(document, "visibilityState", "get").mockReturnValue("visible");
});

afterEach(() => {
  cleanup();
  expect(vi.getTimerCount()).toBe(0);
  vi.restoreAllMocks();
  vi.useRealTimers();
});

describe("useRelativeTimeTick", () => {
  it("shares one clock and stops it after the last subscriber leaves", () => {
    const first = renderHook(useRelativeTimeTick);
    const second = renderHook(useRelativeTimeTick);
    const initial = first.result.current;
    expect(vi.getTimerCount()).toBe(1);
    act(() => vi.advanceTimersByTime(30_000));
    expect(first.result.current).not.toBe(initial);
    expect(second.result.current).toBe(first.result.current);
    first.unmount();
    expect(vi.getTimerCount()).toBe(1);
    second.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("pauses while hidden and immediately catches up when visible or focused", () => {
    const visibility = vi.spyOn(document, "visibilityState", "get");
    const hook = renderHook(useRelativeTimeTick);
    visibility.mockReturnValue("hidden");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    const hidden = hook.result.current;
    expect(vi.getTimerCount()).toBe(0);
    act(() => vi.advanceTimersByTime(600_000));
    expect(hook.result.current).toBe(hidden);
    visibility.mockReturnValue("visible");
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    expect(hook.result.current).not.toBe(hidden);
    expect(vi.getTimerCount()).toBe(1);
    const visible = hook.result.current;
    act(() => window.dispatchEvent(new Event("focus")));
    expect(hook.result.current).not.toBe(visible);
    expect(vi.getTimerCount()).toBe(1);
  });

  it("does not start a timer when first mounted in a hidden document", () => {
    vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    renderHook(useRelativeTimeTick);
    expect(vi.getTimerCount()).toBe(0);
  });

  it("survives StrictMode remounts and removes visibility/focus listeners", () => {
    const removeDocument = vi.spyOn(document, "removeEventListener");
    const removeWindow = vi.spyOn(window, "removeEventListener");
    const hook = renderHook(useRelativeTimeTick, {
      wrapper: ({ children }) => createElement(StrictMode, null, children),
    });
    expect(vi.getTimerCount()).toBe(1);
    hook.unmount();
    expect(removeDocument).toHaveBeenCalledWith("visibilitychange", expect.any(Function));
    expect(removeWindow).toHaveBeenCalledWith("focus", expect.any(Function));
    act(() => document.dispatchEvent(new Event("visibilitychange")));
    act(() => window.dispatchEvent(new Event("focus")));
    expect(vi.getTimerCount()).toBe(0);
    const remounted = renderHook(useRelativeTimeTick);
    const initial = remounted.result.current;
    act(() => vi.advanceTimersByTime(30_000));
    expect(remounted.result.current).not.toBe(initial);
  });
});
