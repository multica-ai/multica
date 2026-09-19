"use client";

import { cn } from "@multica/ui/lib/utils";
import { SidebarTrigger, useSidebarSafe } from "@multica/ui/components/ui/sidebar";

/**
 * The left edge every page shares: the header, the toolbar under it, and any
 * body row that has to line up with them.
 *
 * It is a constant rather than a per-page class because the pages drifted
 * apart exactly when it was one — collection pages sat at `px-5` and the
 * issues family at `px-4`, so switching tabs moved the title 4px. Import this
 * anywhere that edge matters instead of writing the class again; a page that
 * spells its own gutter is the bug coming back.
 */
export const PAGE_GUTTER = "px-4";

/**
 * The centred column a detail page reads inside: `runtimes`, and the agent and
 * skill detail pages. A detail page is a document about one entity, not a
 * table, so past a point more width stops helping and starts stretching a
 * two-column layout apart.
 *
 * Pair it with `PAGE_GUTTER` on the same element, or on a child that already
 * carries the gutter itself (a toolbar, a secondary nav rail). Every band on
 * the page — header, tab bar, banners, each panel — has to read this same
 * constant, because the bug it fixes is chrome sitting on the rail while the
 * content under it does not: below ~1730px they agree, and above it the tabs
 * walk away from what they label (MUL-7107).
 *
 * Full-width borders and backgrounds stay on the outer element; the rail goes
 * on the child that holds the content.
 *
 * Not for list surfaces that are genuinely tables — Issues, My Issues, member
 * detail — which stay full-bleed on `PAGE_GUTTER` alone.
 */
export const PAGE_RAIL = "mx-auto w-full max-w-[1440px]";

/**
 * The filter/actions row directly under a `PageHeader`: same height and
 * gutter so the two read as one chrome block. Shared for the same reason as
 * `PAGE_GUTTER` — the toolbars drifted when each page spelled its own row.
 */
export const PAGE_TOOLBAR = cn(
  "flex h-12 shrink-0 items-center justify-between gap-2",
  PAGE_GUTTER,
);

/**
 * The way back to the nav wherever it is not a permanent column: a sheet below
 * the compact breakpoint, auto-collapsed from there up to `xl`.
 *
 * Every surface below `xl` needs one of these — unless the shell around it
 * already keeps one on screen. `PageHeader` supplies it for free, so this is
 * exported for the pages that build their own chrome instead; without it a
 * touch user has no way to reopen the nav at all.
 *
 * Renders nothing in the two cases where it would not be the way back:
 * outside a `SidebarProvider` (a page that stands alone, with no nav to
 * reopen), and under a shell that declares `hasExternalTrigger` — the desktop
 * window toolbar's trigger never scrolls away or hides, so a second copy in
 * every page header stacked two identical icons 50px apart, and a third
 * whenever a detail pane brought its own header along (MUL-6218).
 */
export function CollapsedNavTrigger() {
  const sidebar = useSidebarSafe();
  if (!sidebar || sidebar.hasExternalTrigger) return null;
  return <SidebarTrigger className="xl:hidden" />;
}

interface PageHeaderProps {
  children: React.ReactNode;
  /**
   * Replaces the mobile sidebar trigger at the far left.
   *
   * For a surface a phone reaches by drilling in rather than by navigating —
   * the inbox's issue detail — "go back" is the leading affordance that
   * matters, and the sidebar is still one step away behind it. Rendering both
   * would spend two of the header's 48px on navigation chrome and leave the
   * title nothing to truncate into.
   */
  leading?: React.ReactNode;
  className?: string;
}

/**
 * Push actions right with `flex-1` on the content group, never with
 * `justify-between` on the header.
 *
 * The leading slot below is a flex item too, so a header that reads as two
 * zones in source is three at runtime, and `justify-between` splits the free
 * space on BOTH sides of the title — parking it mid-header. Desktop windows
 * sit below `xl`, where the trigger renders, so that is where it surfaces.
 *
 * `gap-2` is the base for the same reason: it makes the header's own gap the
 * single source of the leading-slot spacing. When the trigger carried its own
 * margin as well, every header that declared a gap paid both and its title
 * started further right than the ones that declared none. Override the gap
 * per header if the zones need more air; do not add margin to the trigger.
 *
 * Do not pass a `px-*` through `className`. The gutter is `PAGE_GUTTER` for
 * every page, and the toolbar beneath the header reads the same constant.
 */
export function PageHeader({ children, leading, className }: PageHeaderProps) {
  return (
    <header
      className={cn(
        "flex h-12 shrink-0 items-center gap-2 border-b",
        className,
        PAGE_GUTTER,
      )}
    >
      {leading ?? <CollapsedNavTrigger />}
      {children}
    </header>
  );
}
