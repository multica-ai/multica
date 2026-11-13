/**
 * A control in the bottom bar. The bar sits on the app shell like the rail,
 * so its controls take the sidebar accent for the same reason (see
 * `rail-styles.ts`).
 */
export const BOTTOM_BAR_ITEM_CLASS =
  "inline-flex h-6 min-w-0 shrink-0 cursor-pointer items-center gap-1.5 rounded-md px-2 text-caption text-muted-foreground transition-colors outline-none hover:bg-sidebar-accent/70 hover:text-sidebar-accent-foreground focus-visible:outline-2 focus-visible:outline-ring aria-pressed:bg-sidebar-accent aria-pressed:text-sidebar-accent-foreground data-popup-open:bg-sidebar-accent data-popup-open:text-sidebar-accent-foreground [&_svg]:size-3.5 [&_svg]:shrink-0";

export const BOTTOM_BAR_ICON_ITEM_CLASS = `${BOTTOM_BAR_ITEM_CLASS} w-6 justify-center px-0`;
