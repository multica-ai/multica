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
  return intersectionArea(a, b) > 0;
}

function intersectionArea(a: Rectangle, b: Rectangle): number {
  const width = Math.max(0, Math.min(a.x + a.width, b.x + b.width) - Math.max(a.x, b.x));
  const height = Math.max(0, Math.min(a.y + a.height, b.y + b.height) - Math.max(a.y, b.y));
  return width * height;
}

function isUsableWorkArea(wa: DisplayWorkArea | undefined | null): wa is DisplayWorkArea {
  return wa != null && wa.width > 0 && wa.height > 0;
}

/**
 * Resolve persisted geometry against the connected display work areas.
 * Intersecting bounds are resized and repositioned to remain fully visible;
 * disconnected bounds keep their size and state flags but omit coordinates.
 *
 * Size is always clamped to the work area the window will actually land on.
 * When saved bounds intersect no display, Electron centers the window on the
 * primary display, so `primaryWorkArea` is the clamp target for that path.
 * It falls back to the first usable entry in `displays` when not supplied.
 */
export function resolveWindowOptions(
  saved: WindowState,
  displays: DisplayWorkArea[],
  primaryWorkArea?: DisplayWorkArea,
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

  const savedBounds =
    saved.x != null &&
    saved.y != null &&
    saved.width != null &&
    saved.height != null &&
    saved.width > 0 &&
    saved.height > 0
      ? { x: saved.x, y: saved.y, width: saved.width, height: saved.height }
      : null;
  const workArea = savedBounds
    ? displays.reduce<DisplayWorkArea | null>((best, candidate) => {
        if (candidate.width <= 0 || candidate.height <= 0) return best;
        const candidateArea = intersectionArea(savedBounds, candidate);
        if (candidateArea === 0) return best;
        return !best || candidateArea > intersectionArea(savedBounds, best) ? candidate : best;
      }, null)
    : null;

  // Clamp size against the display the window lands on: the intersected one
  // when the saved bounds are still visible, otherwise the primary display
  // Electron will center on. Without this the off-screen and no-saved-state
  // paths could restore a window larger than the current work area (#5244).
  const clampTarget = workArea ?? [primaryWorkArea, ...displays].find(isUsableWorkArea) ?? null;

  const resolvedWidth = clampTarget ? Math.min(width, clampTarget.width) : width;
  const resolvedHeight = clampTarget ? Math.min(height, clampTarget.height) : height;
  return {
    width: resolvedWidth,
    height: resolvedHeight,
    ...(workArea && savedBounds
      ? {
          x: Math.min(
            Math.max(savedBounds.x, workArea.x),
            workArea.x + workArea.width - resolvedWidth,
          ),
          y: Math.min(
            Math.max(savedBounds.y, workArea.y),
            workArea.y + workArea.height - resolvedHeight,
          ),
        }
      : {}),
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
