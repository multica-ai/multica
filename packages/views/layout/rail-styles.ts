/**
 * The rail sits on the app shell, where the page surface tokens are nearly
 * invisible (`--surface-hover` is within a hair of `--app-shell` in light
 * mode), so rail controls use the sidebar accent instead.
 */
/**
 * The space panel's and list columns' collapse motion. Their content is
 * end-aligned, so it slides with the moving edge instead of being wiped.
 */
export const NAV_COLUMN_MOTION_CLASS =
  "transition-[width] duration-(--nav-panel-duration) ease-(--nav-panel-easing) motion-reduce:transition-none";

/** A collapsed panel or list column previewed over the card's leading edge. */
export const NAV_PEEK_CLASS =
  "absolute inset-y-0 left-0 z-30 flex w-(--sidebar-width) flex-col overflow-hidden rounded-xl bg-page-canvas shadow-[var(--floating-shadow)] ring-1 ring-surface-border";

export const RAIL_ICON_BUTTON_CLASS =
  "relative inline-flex size-9 shrink-0 cursor-pointer items-center justify-center rounded-lg text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-ring data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground aria-[current=page]:bg-sidebar-accent aria-[current=page]:text-sidebar-accent-foreground [&_svg:not([class*='size-'])]:size-[18px]";
