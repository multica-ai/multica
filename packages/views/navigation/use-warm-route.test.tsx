import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { renderHook } from "@testing-library/react";
import type { ReactNode } from "react";
import { NavigationProvider } from "./context";
import type { NavigationAdapter } from "./types";
import { useWarmRoute } from "./use-warm-route";

function wrapper(prefetch: NavigationAdapter["prefetch"]) {
  const adapter: NavigationAdapter = {
    push: vi.fn(),
    replace: vi.fn(),
    back: vi.fn(),
    pathname: "/acme/issues",
    searchParams: new URLSearchParams(),
    hash: "",
    getShareableUrl: (p) => p,
    prefetch,
  };
  return function Wrapper({ children }: { children: ReactNode }) {
    return <NavigationProvider value={adapter}>{children}</NavigationProvider>;
  };
}

beforeEach(() => {
  vi.useFakeTimers();
});

afterEach(() => {
  vi.useRealTimers();
});

describe("useWarmRoute", () => {
  it("prefetches one page of a kind once the browser is idle, once per session", () => {
    const prefetch = vi.fn();
    const first = renderHook(() => useWarmRoute("issue-a", "/acme/issues/MUL-1"), { wrapper: wrapper(prefetch) });
    expect(prefetch).not.toHaveBeenCalled();
    vi.advanceTimersByTime(3000);
    expect(prefetch).toHaveBeenCalledWith("/acme/issues/MUL-1");
    first.unmount();

    renderHook(() => useWarmRoute("issue-a", "/acme/issues/MUL-2"), { wrapper: wrapper(prefetch) });
    vi.advanceTimersByTime(3000);
    expect(prefetch).toHaveBeenCalledTimes(1);
  });

  it("warms once when several lists of a kind mount together", () => {
    const prefetch = vi.fn();
    renderHook(
      () => {
        useWarmRoute("issue-d", "/acme/issues/MUL-1");
        useWarmRoute("issue-d", "/acme/issues/MUL-2");
      },
      { wrapper: wrapper(prefetch) },
    );
    vi.advanceTimersByTime(3000);
    expect(prefetch).toHaveBeenCalledTimes(1);
    expect(prefetch).toHaveBeenCalledWith("/acme/issues/MUL-1");
  });

  it("keeps waiting while there is no page to warm", () => {
    const prefetch = vi.fn();
    const { rerender } = renderHook(({ href }: { href: string | null }) => useWarmRoute("issue-b", href), {
      wrapper: wrapper(prefetch),
      initialProps: { href: null as string | null },
    });
    vi.advanceTimersByTime(3000);
    expect(prefetch).not.toHaveBeenCalled();

    rerender({ href: "/acme/issues/MUL-3" });
    vi.advanceTimersByTime(3000);
    expect(prefetch).toHaveBeenCalledWith("/acme/issues/MUL-3");
  });

  it("does nothing without route prefetch (desktop)", () => {
    renderHook(() => useWarmRoute("issue-c", "/acme/issues/MUL-1"), { wrapper: wrapper(undefined) });
    expect(() => vi.advanceTimersByTime(3000)).not.toThrow();
  });
});
