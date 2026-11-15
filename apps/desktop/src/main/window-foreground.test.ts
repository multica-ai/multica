// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import type { BrowserWindow } from "electron";
import { installWindowForegroundEvents, windowForeground } from "./window-foreground";

describe("native window foreground", () => {
  it("requires focus, visibility, and a restored live window", () => {
    const state = { focused: true, visible: true, minimized: false, destroyed: false };
    const window = {
      isFocused: () => state.focused, isVisible: () => state.visible,
      isMinimized: () => state.minimized, isDestroyed: () => state.destroyed,
    } as BrowserWindow;
    expect(windowForeground(window)).toBe(true);
    for (const change of [{ focused: false }, { visible: false }, { minimized: true }, { destroyed: true }]) {
      Object.assign(state, change);
      expect(windowForeground(window)).toBe(false);
      Object.assign(state, { focused: true, visible: true, minimized: false, destroyed: false });
    }
    expect(windowForeground(null)).toBe(false);
  });

  it("reports focus changes from the native window to its own renderer", () => {
    const callbacks = new Map<string, () => void>();
    const send = vi.fn();
    let focused = true;
    const window = {
      isDestroyed: () => false, isVisible: () => true, isMinimized: () => false,
      isFocused: () => focused, on: (event: string, callback: () => void) => callbacks.set(event, callback),
      webContents: { send },
    } as unknown as BrowserWindow;
    installWindowForegroundEvents(window);
    focused = false;
    callbacks.get("blur")!();
    expect(send).toHaveBeenLastCalledWith("window:foreground-changed", false);
    focused = true;
    callbacks.get("focus")!();
    expect(send).toHaveBeenLastCalledWith("window:foreground-changed", true);
  });
});
