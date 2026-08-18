import { describe, expect, it } from "vitest";
import { renderHook } from "@testing-library/react";
import {
  ScrollRestorationProvider,
  useRestoredScrollEntry,
  useRestoredScrollOffset,
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

  it("useScrollRestorationAdapter returns the adapter when provided", () => {
    const adapter = makeAdapter(() => undefined);
    const { result } = renderHook(() => useScrollRestorationAdapter(), {
      wrapper: ({ children }) => (
        <ScrollRestorationProvider adapter={adapter}>
          {children}
        </ScrollRestorationProvider>
      ),
    });
    expect(result.current).toBe(adapter);
  });

  it("useRestoredScrollEntry returns the full entry including contentKey", () => {
    const adapter = makeAdapter(() => ({
      top: 321,
      height: 900,
      contentKey: "ck",
    }));
    const { result } = renderHook(() => useRestoredScrollEntry("html-iframe"), {
      wrapper: ({ children }) => (
        <ScrollRestorationProvider adapter={adapter}>
          {children}
        </ScrollRestorationProvider>
      ),
    });
    expect(result.current).toEqual({ top: 321, height: 900, contentKey: "ck" });
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

  it("all hooks return undefined without a provider (web no-op path)", () => {
    const entry = renderHook(() => useRestoredScrollEntry("x"));
    const offset = renderHook(() => useRestoredScrollOffset("x"));
    expect(entry.result.current).toBeUndefined();
    expect(offset.result.current).toBeUndefined();
  });
});
