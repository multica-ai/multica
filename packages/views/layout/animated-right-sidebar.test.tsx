import { act, renderHook } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";
import {
  configureShortcutPlatform,
  useShortcutStore,
} from "@multica/core/shortcuts";

import {
  getAnimatedRightSidebarInitialOpen,
  useAnimatedRightSidebarState,
  useRightSidebarShortcut,
} from "./animated-right-sidebar";

afterEach(() => {
  configureShortcutPlatform(null);
  useShortcutStore.getState().resetAll();
});

describe("animated right sidebar state", () => {
  it("uses a restored collapsed layout before falling back to the default", () => {
    expect(
      getAnimatedRightSidebarInitialOpen(true, {
        content: 100,
        sidebar: 0,
      }),
    ).toBe(false);
  });

  it("uses a restored expanded layout before falling back to the default", () => {
    expect(
      getAnimatedRightSidebarInitialOpen(false, {
        content: 70,
        sidebar: 30,
      }),
    ).toBe(true);
  });

  it("falls back to the caller default when no sidebar layout was restored", () => {
    expect(getAnimatedRightSidebarInitialOpen(true, undefined)).toBe(true);
    expect(getAnimatedRightSidebarInitialOpen(false, { content: 100 })).toBe(false);
  });

  it("treats a non-zero layout percentage as open even before pixels are measured", () => {
    const { result } = renderHook(() => useAnimatedRightSidebarState(false));

    act(() => {
      result.current.handleResize({
        asPercentage: 30,
        inPixels: 0,
      });
    });

    expect(result.current.open).toBe(true);
    expect(result.current.visualOpen).toBe(true);
    expect(result.current.motionEnabled).toBe(false);
  });

  it("enables motion only for an explicit toggle window", () => {
    vi.useFakeTimers();
    try {
      const { result } = renderHook(() => useAnimatedRightSidebarState(false));

      expect(result.current.motionEnabled).toBe(false);

      act(() => {
        result.current.beginToggle(true);
      });

      expect(result.current.open).toBe(true);
      expect(result.current.visualOpen).toBe(true);
      expect(result.current.motionEnabled).toBe(true);

      act(() => {
        vi.runAllTimers();
      });

      expect(result.current.motionEnabled).toBe(false);
    } finally {
      vi.useRealTimers();
    }
  });
});

describe("right sidebar shortcut", () => {
  it("toggles the visible detail sidebar on Mod+/", () => {
    configureShortcutPlatform("macos");
    const onToggle = vi.fn();
    const target = document.createElement("div");
    vi.spyOn(target, "getClientRects").mockReturnValue(
      [{}] as unknown as DOMRectList,
    );
    const targetRef = { current: target };
    const { unmount } = renderHook(() =>
      useRightSidebarShortcut(targetRef, onToggle),
    );
    const event = new KeyboardEvent("keydown", {
      key: "/",
      metaKey: true,
      cancelable: true,
    });

    document.dispatchEvent(event);

    expect(onToggle).toHaveBeenCalledTimes(1);
    expect(event.defaultPrevented).toBe(true);
    unmount();
  });

  it("leaves the shortcut unclaimed when its detail page is hidden", () => {
    configureShortcutPlatform("macos");
    const onToggle = vi.fn();
    const target = document.createElement("div");
    const targetRef = { current: target };
    const { unmount } = renderHook(() =>
      useRightSidebarShortcut(targetRef, onToggle),
    );
    const event = new KeyboardEvent("keydown", {
      key: "/",
      metaKey: true,
      cancelable: true,
    });

    document.dispatchEvent(event);

    expect(onToggle).not.toHaveBeenCalled();
    expect(event.defaultPrevented).toBe(false);
    unmount();
  });
});
