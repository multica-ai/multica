import { describe, it, expect, afterEach } from "vitest";
import { renderHook, act } from "@testing-library/react";
import { useIsMobile, useIsCompact } from "@multica/ui/hooks/use-mobile";

describe("useIsMobile and useIsCompact", () => {
  const originalInnerWidth = window.innerWidth;

  afterEach(() => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: originalInnerWidth,
      writable: true,
    });
  });

  function setWidth(width: number) {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: width,
      writable: true,
    });
    act(() => {
      window.dispatchEvent(new Event("resize"));
    });
  }

  it("evaluates mobile synchronously on initial render without a false->true flip", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 390,
      writable: true,
    });

    const { result } = renderHook(() => useIsMobile());

    // Synchronous evaluation on first render: must be true immediately
    expect(result.current).toBe(true);
  });

  it("evaluates desktop synchronously on initial render", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 1280,
      writable: true,
    });

    const { result } = renderHook(() => useIsMobile());

    expect(result.current).toBe(false);
  });

  it("evaluates compact breakpoint correctly (< 1024px)", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 800,
      writable: true,
    });

    const { result: isMobileResult } = renderHook(() => useIsMobile());
    const { result: isCompactResult } = renderHook(() => useIsCompact());

    expect(isMobileResult.current).toBe(false);
    expect(isCompactResult.current).toBe(true);
  });

  it("updates synchronously when viewport width changes via resize event", () => {
    Object.defineProperty(window, "innerWidth", {
      configurable: true,
      value: 1280,
      writable: true,
    });

    const { result } = renderHook(() => useIsMobile());
    expect(result.current).toBe(false);

    setWidth(390);
    expect(result.current).toBe(true);

    setWidth(1024);
    expect(result.current).toBe(false);
  });
});
