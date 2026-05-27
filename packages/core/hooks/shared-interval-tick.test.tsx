/**
 * @vitest-environment jsdom
 */
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { createSharedIntervalTick } from "./shared-interval-tick";

describe("createSharedIntervalTick", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it("uses one interval for all subscribers and clears it after the last unsubscribe", () => {
    const setIntervalSpy = vi.spyOn(globalThis, "setInterval");
    const clearIntervalSpy = vi.spyOn(globalThis, "clearInterval");
    const useTick = createSharedIntervalTick(1000);

    const first = renderHook(() => useTick());
    const second = renderHook(() => useTick());

    expect(setIntervalSpy).toHaveBeenCalledTimes(1);
    expect(first.result.current).toBe(0);
    expect(second.result.current).toBe(0);

    act(() => {
      vi.advanceTimersByTime(1000);
    });

    expect(first.result.current).toBe(1);
    expect(second.result.current).toBe(1);

    first.unmount();
    expect(clearIntervalSpy).not.toHaveBeenCalled();

    second.unmount();
    expect(clearIntervalSpy).toHaveBeenCalledTimes(1);
  });
});
