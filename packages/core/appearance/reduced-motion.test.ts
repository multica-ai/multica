// @vitest-environment jsdom
import { act, renderHook } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { prefersReducedMotion, useAppearanceStore, useReducedMotion } from "./index";

let systemReduces = false;
const listeners = new Set<() => void>();

function setSystem(reduces: boolean) {
  systemReduces = reduces;
  for (const listener of listeners) listener();
}

beforeEach(() => {
  systemReduces = false;
  listeners.clear();
  vi.stubGlobal("matchMedia", (query: string) => ({
    get matches() {
      return query === "(prefers-reduced-motion: reduce)" && systemReduces;
    },
    media: query,
    addEventListener: (_: string, listener: () => void) => listeners.add(listener),
    removeEventListener: (_: string, listener: () => void) => listeners.delete(listener),
  }));
  useAppearanceStore.getState().reset();
});

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("reduced motion", () => {
  it("follows the Appearance switch while the system allows motion", () => {
    const { result } = renderHook(() => useReducedMotion());
    expect(result.current).toBe(false);
    expect(prefersReducedMotion()).toBe(false);

    act(() => useAppearanceStore.getState().set({ reduceMotion: true }));
    expect(result.current).toBe(true);
    expect(prefersReducedMotion()).toBe(true);

    act(() => useAppearanceStore.getState().set({ reduceMotion: false }));
    expect(result.current).toBe(false);
  });

  it("follows the system setting while the Appearance switch is off", () => {
    const { result } = renderHook(() => useReducedMotion());
    act(() => setSystem(true));
    expect(result.current).toBe(true);
    expect(prefersReducedMotion()).toBe(true);

    act(() => setSystem(false));
    expect(result.current).toBe(false);
  });

  it("stays reduced while either one asks for it", () => {
    setSystem(true);
    const { result } = renderHook(() => useReducedMotion());
    act(() => useAppearanceStore.getState().set({ reduceMotion: true }));
    act(() => setSystem(false));
    expect(result.current).toBe(true);
  });
});
