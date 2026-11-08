import { readFileSync, writeFileSync } from "node:fs";
import { join } from "node:path";
import { ZOOM_MAX, ZOOM_MIN } from "./keyboard-shortcuts";

// Chromium keeps zoom per origin in memory only, so Cmd/Ctrl +/- resets to
// 100% on every launch. Persist the level beside window-state.json instead.
export const ZOOM_LEVEL_FILENAME = "zoom-level.json";

export function zoomLevelFilePath(userDataPath: string): string {
  return join(userDataPath, ZOOM_LEVEL_FILENAME);
}

/** Parse a saved zoom level; anything unusable falls back to 0 (100%). */
export function parseZoomLevel(raw: string): number {
  try {
    const level: unknown = (JSON.parse(raw) as { level?: unknown })?.level;
    if (typeof level !== "number" || !Number.isFinite(level)) return 0;
    return Math.min(Math.max(level, ZOOM_MIN), ZOOM_MAX);
  } catch {
    return 0;
  }
}

export function loadZoomLevel(filePath: string): number {
  try {
    return parseZoomLevel(readFileSync(filePath, "utf8"));
  } catch {
    return 0;
  }
}

export function saveZoomLevel(filePath: string, level: number): void {
  try {
    writeFileSync(filePath, JSON.stringify({ level }), "utf8");
  } catch {
    // Disk full / permissions — drop silently; next launch uses 100%.
  }
}
