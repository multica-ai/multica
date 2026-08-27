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
 * to "push". On compact web (no session tabs), surfaces that render a real
 * anchor should leave any modified click to the browser — native handling is
 * the only way to get a true background *browser* tab there. Wide web and
 * desktop implement `openInNewTab` and consume this function.
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
