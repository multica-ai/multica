/**
 * Coarse platform detection for keyboard-shortcut display.
 *
 * Eagerly evaluated at module load. On the server (no `navigator`) this
 * resolves to `false`, so SSR always renders the non-Mac variant; on a
 * real Mac the value is true after hydration. Acceptable trade-off for
 * cosmetic shortcut hints — never gate functional behavior on this.
 */
export const isMac =
  typeof navigator !== "undefined" && /Mac/.test(navigator.platform);

/** Modifier key label — ⌘ on Mac, "Ctrl" elsewhere. */
export const modKey: string = isMac ? "⌘" : "Ctrl";

/** Enter / return key label — ↵ on Mac, "Enter" elsewhere. */
export const enterKey: string = isMac ? "↵" : "Enter";

/**
 * Join key labels for display. Mac compresses combos with no separator
 * ("⌘K", "⌘↵"); other platforms use "+" ("Ctrl+K", "Ctrl+Enter").
 */
export function formatShortcut(...keys: string[]): string {
  return keys.join(isMac ? "" : "+");
}
