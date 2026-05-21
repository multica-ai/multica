import type { BrowserWindow } from "electron";

export interface RendererFreezeWatchdogOptions {
  enabled?: boolean;
  timeoutMs?: number;
  logger?: Pick<Console, "info" | "warn" | "error">;
  timers?: Pick<typeof globalThis, "setTimeout" | "clearTimeout">;
}

const DEFAULT_TIMEOUT_MS = 15_000;

export function installRendererFreezeWatchdog(
  win: BrowserWindow,
  options: RendererFreezeWatchdogOptions = {},
): void {
  if (options.enabled === false) return;

  const timeoutMs = options.timeoutMs ?? DEFAULT_TIMEOUT_MS;
  const logger = options.logger ?? console;
  const timers = options.timers ?? globalThis;
  let unresponsive = false;
  let reloadTimer: ReturnType<typeof setTimeout> | null = null;

  const clearReloadTimer = () => {
    if (!reloadTimer) return;
    timers.clearTimeout(reloadTimer);
    reloadTimer = null;
  };

  win.on("unresponsive", () => {
    unresponsive = true;
    clearReloadTimer();
    logger.warn("[renderer] window became unresponsive");
    reloadTimer = timers.setTimeout(() => {
      reloadTimer = null;
      if (!unresponsive || win.isDestroyed()) return;
      const webContents = win.webContents;
      if (webContents.isDestroyed()) return;
      logger.warn(
        `[renderer] still unresponsive after ${timeoutMs}ms; reloading renderer`,
      );
      webContents.reloadIgnoringCache();
    }, timeoutMs);
  });

  win.on("responsive", () => {
    if (unresponsive) {
      logger.info("[renderer] window became responsive again");
    }
    unresponsive = false;
    clearReloadTimer();
  });

  win.webContents.on("render-process-gone", (_event, details) => {
    logger.error("[renderer] render process gone", details);
  });

  win.on("closed", clearReloadTimer);
}
