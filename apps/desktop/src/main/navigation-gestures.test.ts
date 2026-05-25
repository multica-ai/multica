import type { BrowserWindow } from "electron";
import { describe, expect, it, vi } from "vitest";
import { NAVIGATION_GESTURE_CHANNEL } from "../shared/navigation-gestures";
import { installNavigationGestures } from "./navigation-gestures";

function makeWindow() {
  let swipeHandler:
    | ((event: unknown, direction: string) => void)
    | undefined;

  const win = {
    on: vi.fn(
      (event: string, handler: (event: unknown, direction: string) => void) => {
        if (event === "swipe") swipeHandler = handler;
        return win;
      },
    ),
    webContents: {
      send: vi.fn(),
    },
  };

  return {
    win: win as unknown as BrowserWindow,
    send: win.webContents.send,
    emitSwipe: (direction: string) => swipeHandler?.({}, direction),
  };
}

describe("installNavigationGestures", () => {
  it("registers macOS swipe navigation", () => {
    const { win, send, emitSwipe } = makeWindow();

    installNavigationGestures(win, "darwin");

    emitSwipe("right");
    expect(send).toHaveBeenCalledWith(NAVIGATION_GESTURE_CHANNEL, "back");

    emitSwipe("left");
    expect(send).toHaveBeenCalledWith(NAVIGATION_GESTURE_CHANNEL, "forward");
  });

  it("ignores non-horizontal swipe directions", () => {
    const { win, send, emitSwipe } = makeWindow();

    installNavigationGestures(win, "darwin");
    emitSwipe("up");

    expect(send).not.toHaveBeenCalled();
  });

  it("does not register on non-mac platforms", () => {
    const { win, send, emitSwipe } = makeWindow();

    installNavigationGestures(win, "linux");
    emitSwipe("right");

    expect(send).not.toHaveBeenCalled();
  });
});
