import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import { useAppForeground } from "./use-app-foreground";

/** Drive jsdom's visibility + focus, then fire the event the hook listens on. */
function setEnv(visible: boolean, focused: boolean) {
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => (visible ? "visible" : "hidden"),
  });
  vi.spyOn(document, "hasFocus").mockReturnValue(focused);
}

afterEach(() => {
  vi.restoreAllMocks();
  Object.defineProperty(document, "visibilityState", {
    configurable: true,
    get: () => "visible",
  });
});

describe("useAppForeground", () => {
  it("is true when the document is visible and focused", () => {
    setEnv(true, true);
    const { result } = renderHook(() => useAppForeground());
    expect(result.current).toBe(true);
  });

  it("is false when the window loses focus (another app / window on top)", () => {
    setEnv(true, false);
    const { result } = renderHook(() => useAppForeground());
    expect(result.current).toBe(false);
  });

  it("is false when the tab is hidden even if it still reports focus", () => {
    setEnv(false, true);
    const { result } = renderHook(() => useAppForeground());
    expect(result.current).toBe(false);
  });

  it("reacts to blur and focus events", () => {
    setEnv(true, true);
    const { result } = renderHook(() => useAppForeground());
    expect(result.current).toBe(true);

    act(() => {
      setEnv(true, false);
      window.dispatchEvent(new Event("blur"));
    });
    expect(result.current).toBe(false);

    act(() => {
      setEnv(true, true);
      window.dispatchEvent(new Event("focus"));
    });
    expect(result.current).toBe(true);
  });

  it("reacts to visibilitychange events", () => {
    setEnv(true, true);
    const { result } = renderHook(() => useAppForeground());
    expect(result.current).toBe(true);

    act(() => {
      setEnv(false, true);
      document.dispatchEvent(new Event("visibilitychange"));
    });
    expect(result.current).toBe(false);
  });
});
