/**
 * The one place a mouse event is mapped to a navigation intent, per the
 * navigation behavior spec:
 *
 *   plain click        -> "push"            (navigate in place)
 *   cmd/ctrl + click   -> "background-tab"  (open tab, keep focus here)
 *   cmd/ctrl + shift   -> "foreground-tab"  (open tab, move focus there)
 *   middle click       -> "background-tab"
 *
 * Shift alone is deliberately NOT a modifier: browsers disagree about it
 * (Chrome/Firefox open a new window, Safari adds to Reading List), so it maps
 * to "push". On web, surfaces that render a real anchor should leave any
 * modified click to the browser instead of consulting this function — native
 * handling is the only way to get a true background tab there.
 */
export type LinkClickIntent = "push" | "background-tab" | "foreground-tab";

export function resolveClickIntent(
  e: Pick<MouseEvent, "button" | "metaKey" | "ctrlKey" | "shiftKey">,
): LinkClickIntent {
  if (e.button === 1) return "background-tab";
  if (e.metaKey || e.ctrlKey) {
    return e.shiftKey ? "foreground-tab" : "background-tab";
  }
  return "push";
}
