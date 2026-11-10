import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import common from "../locales/en/common.json";
import { useTimeAgo } from "./use-time-ago";

vi.mock("./use-t", () => ({
  useT: () => ({
    t: (
      selector: (resource: typeof common) => string,
      options?: { count?: number },
    ) => {
      const template = selector(common);
      return options?.count === undefined
        ? template
        : template.replace("{{count}}", String(options.count));
    },
  }),
}));

const MINUTE = 60_000;

function isoMinutesAgo(minutes: number): string {
  return new Date(Date.now() - minutes * MINUTE).toISOString();
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.clearAllTimers();
  vi.useRealTimers();
});

describe("useTimeAgo", () => {
  it("refreshes a memoized caller when the shared clock advances a minute", () => {
    const { result } = renderHook(() => useTimeAgo());
    const stamp = isoMinutesAgo(0);

    expect(result.current(stamp)).toBe("just now");

    act(() => {
      vi.advanceTimersByTime(MINUTE);
    });

    expect(result.current(stamp)).toBe("1m ago");
  });

  it("starts at one minute ago and steps to the next bucket", () => {
    const stamp = isoMinutesAgo(1);
    const { result } = renderHook(() => useTimeAgo());

    expect(result.current(stamp)).toBe("1m ago");

    act(() => {
      vi.advanceTimersByTime(MINUTE);
    });

    expect(result.current(stamp)).toBe("2m ago");
  });

  it("formats against the clock at mount, not at module import", () => {
    // The module was imported long before this hook mounted; a long-lived SSR
    // process or a test file with import-time fixtures looks exactly like this.
    vi.setSystemTime(new Date("2026-01-01T00:10:00Z"));
    const { result } = renderHook(() => useTimeAgo());

    expect(result.current("2026-01-01T00:00:00Z")).toBe("10m ago");
  });

  it("clears the shared interval once the last subscriber unmounts", () => {
    const first = renderHook(() => useTimeAgo());
    const second = renderHook(() => useTimeAgo());
    expect(vi.getTimerCount()).toBe(1);

    first.unmount();
    expect(vi.getTimerCount()).toBe(1);

    second.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });

  it("keeps a single interval while several components are mounted", () => {
    const mounted = [
      renderHook(() => useTimeAgo()),
      renderHook(() => useTimeAgo()),
      renderHook(() => useTimeAgo()),
    ];

    expect(vi.getTimerCount()).toBe(1);

    for (const hook of mounted) hook.unmount();
    expect(vi.getTimerCount()).toBe(0);
  });
});
