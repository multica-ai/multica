/**
 * Format a hotkey string for display using the platform-correct symbols.
 *
 * Delegates to TanStack's `formatForDisplay` which already handles
 * ⌘/⇧/⌥/⌃ on Mac vs Ctrl+Shift+Alt on other platforms. We re-export
 * a thin wrapper so consumers import from one place and we can inject
 * telemetry or override behaviour later without touching call-sites.
 */
export { formatForDisplay as formatKeys } from "@tanstack/hotkeys";

import { formatForDisplay, formatHotkeySequence } from "@tanstack/hotkeys";
import type { Hotkey } from "@tanstack/hotkeys";
import { isMac } from "../platform/keyboard";

/**
 * Format a hotkey or sequence for display with automatic platform detection.
 *
 * @example
 *   formatKeysForPlatform("Mod+K")       // "⌘ K" on Mac, "Ctrl+K" elsewhere
 *   formatKeysForPlatform(["G", "I"])     // "G I"
 */
export function formatKeysForPlatform(
  keys: string | Hotkey[],
): string {
  if (Array.isArray(keys)) {
    return formatHotkeySequence(keys);
  }
  return formatForDisplay(keys, {
    platform: isMac ? "mac" : "windows",
  });
}
