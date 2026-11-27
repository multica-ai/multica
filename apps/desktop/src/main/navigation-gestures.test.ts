// @vitest-environment node
import { EventEmitter } from "node:events";
import type { BrowserWindow } from "electron";
import { describe, expect, it, vi } from "vitest";
import { NAVIGATION_GESTURE_CHANNEL } from "../shared/navigation-gestures";
import { installNavigationGestures } from "./navigation-gestures";

function makeWindow() {
  const send = vi.fn();
  const webContents = Object.assign(new EventEmitter(), { send });
  const win = Object.assign(new EventEmitter(), { webContents });

  return {
    win: win as unknown as BrowserWindow,
    send,
    emitSwipe: (direction: string) => win.emit("swipe", {}, direction),
    emitAppCommand: (command: string) => win.emit("app-command", {}, command),
    emitMouse: (type: string, button: string) => {
      const event = { preventDefault: vi.fn() };
      webContents.emit("before-mouse-event", event, { type, button });
      return event;
    },
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

  it("ignores swipe events on non-mac platforms", () => {
    const { win, send, emitSwipe } = makeWindow();

    installNavigationGestures(win, "linux");
    emitSwipe("right");

    expect(send).not.toHaveBeenCalled();
  });

  it.each(["darwin", "win32", "linux"] as const)(
    "sends one history action per side button release on %s",
    (platform) => {
      const { win, send, emitMouse } = makeWindow();

      installNavigationGestures(win, platform);

      expect(emitMouse("mouseDown", "back").preventDefault).toHaveBeenCalled();
      expect(send).not.toHaveBeenCalled();
      expect(emitMouse("mouseUp", "back").preventDefault).toHaveBeenCalled();
      expect(emitMouse("mouseDown", "forward").preventDefault).toHaveBeenCalled();
      expect(emitMouse("mouseUp", "forward").preventDefault).toHaveBeenCalled();

      expect(send.mock.calls).toEqual([
        [NAVIGATION_GESTURE_CHANNEL, "back"],
        [NAVIGATION_GESTURE_CHANNEL, "forward"],
      ]);
    },
  );

  it("leaves other mouse input alone", () => {
    const { win, send, emitMouse } = makeWindow();

    installNavigationGestures(win, "darwin");

    expect(emitMouse("mouseUp", "middle").preventDefault).not.toHaveBeenCalled();
    expect(emitMouse("mouseMove", "back").preventDefault).not.toHaveBeenCalled();
    expect(send).not.toHaveBeenCalled();
  });

  it("forwards only Windows browser history app commands", () => {
    const { win, send, emitAppCommand } = makeWindow();

    installNavigationGestures(win, "win32");

    emitAppCommand("browser-backward");
    emitAppCommand("browser-forward");
    emitAppCommand("browser-refresh");

    expect(send.mock.calls).toEqual([
      [NAVIGATION_GESTURE_CHANNEL, "back"],
      [NAVIGATION_GESTURE_CHANNEL, "forward"],
    ]);
  });

  it("ignores browser app commands on Linux", () => {
    const { win, send, emitAppCommand } = makeWindow();

    installNavigationGestures(win, "linux");

    emitAppCommand("browser-backward");
    expect(send).not.toHaveBeenCalled();
  });
});
