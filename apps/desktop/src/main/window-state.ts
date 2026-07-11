import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";

/** Persisted main-window geometry for the next launch (#5244). */
export type WindowState = {
  x?: number;
  y?: number;
  width?: number;
  height?: number;
  isMaximized?: boolean;
  isFullScreen?: boolean;
};

export type Rectangle = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export type DisplayWorkArea = {
  x: number;
  y: number;
  width: number;
  height: number;
};

export const DEFAULT_WINDOW_WIDTH = 1280;
export const DEFAULT_WINDOW_HEIGHT = 800;
export const MIN_WINDOW_WIDTH = 900;
export const MIN_WINDOW_HEIGHT = 600;

export const WINDOW_STATE_FILENAME = "window-state.json";

export function windowStateFilePath(userDataPath: string): string {
  return join(userDataPath, WINDOW_STATE_FILENAME);
}

/**
 * Parse a previously saved window-state JSON blob. Returns `{}` on any
 * failure so a corrupt file never blocks app launch.
 */
export function parseWindowState(raw: string): WindowState {
  try {
    const parsed: unknown = JSON.parse(raw);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      return {};
    }
    const obj = parsed as Record<string, unknown>;
    const out: WindowState = {};
    for (const key of ["x", "y", "width", "height"] as const) {
      const v = obj[key];
      if (typeof v === "number" && Number.isFinite(v)) {
        out[key] = Math.round(v);
      }
    }
    if (typeof obj.isMaximized === "boolean") out.isMaximized = obj.isMaximized;
    if (typeof obj.isFullScreen === "boolean") out.isFullScreen = obj.isFullScreen;
    return out;
  } catch {
    return {};
  }
}

export function loadWindowState(filePath: string): WindowState {
  try {
    return parseWindowState(readFileSync(filePath, "utf8"));
  } catch {
    return {};
  }
}

export function saveWindowStateToFile(filePath: string, state: WindowState): void {
  try {
    writeFileSync(filePath, JSON.stringify(state), "utf8");
  } catch {
    // Disk full / permissions — drop silently; next launch uses defaults.
  }
}

/**
 * True when the rectangle intersects at least one display work area.
 * Prevents restoring a window onto a disconnected external monitor.
 */
export function isVisibleOnSomeDisplay(
  bounds: { x?: number; y?: number; width?: number; height?: number },
  displays: DisplayWorkArea[],
): boolean {
  if (
    bounds.x == null ||
    bounds.y == null ||
    bounds.width == null ||
    bounds.height == null ||
    bounds.width <= 0 ||
    bounds.height <= 0
  ) {
    return false;
  }
  const b = {
    x: bounds.x,
    y: bounds.y,
    width: bounds.width,
    height: bounds.height,
  };
  return displays.some((wa) => rectanglesIntersect(b, wa));
}

export function rectanglesIntersect(a: Rectangle, b: Rectangle): boolean {
  return (
    a.x < b.x + b.width &&
    a.x + a.width > b.x &&
    a.y < b.y + b.height &&
    a.y + a.height > b.y
  );
}

/**
 * Clamp width/height to the app minimums and fall back to defaults when
 * missing or invalid. Position is only applied when `usePosition` is true
 * (caller must validate visibility).
 */
export function resolveWindowOptions(
  saved: WindowState,
  usePosition: boolean,
): {
  width: number;
  height: number;
  x?: number;
  y?: number;
  isMaximized: boolean;
  isFullScreen: boolean;
} {
  const width = Math.max(
    MIN_WINDOW_WIDTH,
    typeof saved.width === "number" && saved.width > 0 ? saved.width : DEFAULT_WINDOW_WIDTH,
  );
  const height = Math.max(
    MIN_WINDOW_HEIGHT,
    typeof saved.height === "number" && saved.height > 0 ? saved.height : DEFAULT_WINDOW_HEIGHT,
  );
  return {
    width,
    height,
    ...(usePosition && saved.x != null && saved.y != null ? { x: saved.x, y: saved.y } : {}),
    isMaximized: saved.isMaximized === true,
    isFullScreen: saved.isFullScreen === true,
  };
}

/** Snapshot used when writing window-state.json from a live BrowserWindow. */
export function snapshotWindowState(win: {
  isDestroyed: () => boolean;
  getNormalBounds: () => Rectangle;
  isMaximized: () => boolean;
  isFullScreen: () => boolean;
}): WindowState | null {
  if (!win || win.isDestroyed()) return null;
  try {
    const bounds = win.getNormalBounds();
    return {
      x: bounds.x,
      y: bounds.y,
      width: bounds.width,
      height: bounds.height,
      isMaximized: win.isMaximized(),
      isFullScreen: win.isFullScreen(),
    };
  } catch {
    return null;
  }
}
