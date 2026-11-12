import type { BrowserWindow } from "electron";

export function windowForeground(window: BrowserWindow | null): boolean {
  return !!window && !window.isDestroyed() && window.isVisible() &&
    !window.isMinimized() && window.isFocused();
}

export function installWindowForegroundEvents(window: BrowserWindow): void {
  const report = () => {
    if (!window.isDestroyed()) {
      window.webContents.send("window:foreground-changed", windowForeground(window));
    }
  };
  window.on("focus", report);
  window.on("blur", report);
  window.on("show", report);
  window.on("hide", report);
  window.on("minimize", report);
  window.on("restore", report);
}
