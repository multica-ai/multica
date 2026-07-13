import type { WebContents } from "electron";

// Shape of the input subset we read from Electron's `before-input-event`.
// Modeled as a structural type so the handler is unit-testable without a
// real Electron Input instance.
export type ShortcutInput = {
  type: string;
  key: string;
  control: boolean;
  meta: boolean;
  alt: boolean;
  shift: boolean;
  isAutoRepeat?: boolean;
};

// Subset of WebContents the zoom handler needs. Keeps the test mock tiny.
export type ZoomTarget = Pick<WebContents, "getZoomLevel" | "setZoomLevel">;

// Match Electron's built-in zoomIn/zoomOut roles (Chromium default of 0.5
// per step). Clamp to a range that keeps the UI legible — values outside
// this band turn the workspace into either confetti or a microfiche.
const ZOOM_STEP = 0.5;
const ZOOM_MIN = -3;
const ZOOM_MAX = 4.5;

/**
 * Inspect a `before-input-event` key and apply (or block) the matching
 * window-level shortcut. Returns `true` when the caller should call
 * `event.preventDefault()` — that both swallows the renderer keydown and
 * prevents the application menu accelerator from firing, so we don't
 * double-trigger zoom on macOS where the default menu also binds these
 * keys.
 *
 * Why we don't rely on the menu's `zoomIn` / `zoomOut` roles: on macOS the
 * default `Cmd+-` accelerator does not fire reliably across keyboard
 * layouts (issue MUL-2354 — Cmd+= zooms in but Cmd+- doesn't undo it).
 * Handling the shortcuts here gives identical behavior on every platform
 * and every layout.
 */
/**
 * Result of handleAppShortcut:
 * - `false`: not handled, let Electron continue
 * - `true`: handled (preventDefault), no further action
 * - `"close-tab"`: Cmd/Ctrl+W intercepted — caller should send IPC to renderer
 */
export type ShortcutResult = boolean | "close-tab";

export function handleAppShortcut(
  input: ShortcutInput,
  webContents: ZoomTarget,
  platform: NodeJS.Platform = process.platform,
): ShortcutResult {
  if (input.type !== "keyDown") return false;
  const primary = platform === "darwin" ? input.meta : input.control;
  const secondary = platform === "darwin" ? input.control : input.meta;
  const noSecondaryModifiers = !secondary && !input.alt;

  // Block reload — accidental Cmd+R / Ctrl+R / F5 destroys in-memory state
  // (tabs, drafts, WS connections) with no URL bar to recover from.
  if ((primary && input.key.toLowerCase() === "r") || input.key === "F5") {
    return true;
  }

  if (!primary || !noSecondaryModifiers) return false;

  // Cmd/Ctrl + "=" (unshifted) or "+" (Shift+=) → zoom in.
  if (
    (input.key === "=" && !input.shift) ||
    (input.key === "+" && input.shift)
  ) {
    const next = Math.min(webContents.getZoomLevel() + ZOOM_STEP, ZOOM_MAX);
    webContents.setZoomLevel(next);
    return true;
  }

  // Cmd/Ctrl + "-" (unshifted) or "_" (Shift+-) → zoom out.
  if (
    (input.key === "-" && !input.shift) ||
    (input.key === "_" && input.shift)
  ) {
    const next = Math.max(webContents.getZoomLevel() - ZOOM_STEP, ZOOM_MIN);
    webContents.setZoomLevel(next);
    return true;
  }

  // Cmd/Ctrl + 0 → reset zoom to 100%.
  if (input.key === "0" && !input.shift) {
    webContents.setZoomLevel(0);
    return true;
  }

  // Cmd/Ctrl + W → close active tab (or window if last tab).
  // Cmd/Ctrl + Shift + W is reserved for "close window" — do not intercept.
  // Return a signal so the caller can send IPC to the renderer.
  if (input.key.toLowerCase() === "w" && !input.shift) {
    // Holding Cmd/Ctrl+W must not race through several product tabs. Swallow
    // Electron's repeated keydown without issuing another close request.
    if (input.isAutoRepeat) return true;
    return "close-tab";
  }

  return false;
}
