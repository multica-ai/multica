/**
 * CSS Custom Highlight API rules for in-page find (Cmd/Ctrl+F).
 *
 * LightningCSS (Next/Turbopack) cannot parse `::highlight()`, so these
 * cannot live in the imported stylesheet bundle. Apps inject the string
 * with a `<style>` tag instead.
 *
 * `multica-find-active` is registered with a higher priority so the current
 * match paints on top of the dimmer all-matches tint.
 */
export const FIND_HIGHLIGHT_CSS = `::highlight(multica-find) {
  background-color: var(--find-match);
  color: var(--find-match-foreground);
}
::highlight(multica-find-active) {
  background-color: var(--find-match-active);
  color: var(--find-match-foreground);
}
`;
