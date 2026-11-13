"use client";

import { createContext, use, useCallback, useEffect, useMemo, useRef, useState } from "react";
import { spaceForPath, type AppSpaceId } from "@multica/core/paths";
import { useSidebarSafe } from "@multica/ui/components/ui/sidebar";
import { useNavigation } from "../navigation";

/**
 * Spaces whose navigation lives in the shell panel. Inbox, Chat and Settings
 * are list-and-detail pages that already render their own list column in the
 * same card, so the shell adds no panel for them.
 */
export const PANEL_SPACES: ReadonlySet<AppSpaceId> = new Set(["work", "team", "autopilots"]);

// Opening waits for the pointer to settle so sweeping across the rail does
// not flash every panel; closing waits long enough to cross the gap between
// the rail and the preview.
const PEEK_OPEN_DELAY_MS = 150;
const PEEK_CLOSE_DELAY_MS = 200;

interface AppShellContextValue {
  space: AppSpaceId | null;
  /** The current space has a shell panel (collapsible with the sidebar toggle). */
  hasPanel: boolean;
  /**
   * Inbox, Chat and Settings keep their own list column, which collapses with
   * the space panel. Tracked apart from the panel's open state so the md–lg
   * auto-collapse never hides a list the page needs.
   */
  listCollapsed: boolean;
  setListCollapsed: (collapsed: boolean) => void;
  /** The sidebar toggle: the space panel and list columns together, or the compact sheet. */
  toggleNav: () => void;
  /**
   * The current space's nav (its panel or list column) is off screen, so the
   * rail previews spaces on hover. Always false in the compact layout.
   */
  navCollapsed: boolean;
  /**
   * Where a rail item leads: the last page visited in that space of this
   * workspace, or `home` for the current space and spaces not yet visited.
   */
  spaceHref: (space: AppSpaceId, home: string) => string;
  /** Space previewed over the content while the nav is collapsed. */
  peekSpace: AppSpaceId | null;
  requestPeek: (space: AppSpaceId) => void;
  holdPeek: () => void;
  releasePeek: () => void;
  closePeek: () => void;
}

const AppShellContext = createContext<AppShellContextValue | null>(null);

export function AppShellProvider({
  renderedPathname,
  children,
}: {
  /**
   * The pathname of the page on screen, where it can trail the navigation's
   * (desktop renders the next page in a transition). The panel and list
   * columns follow it so they never frame the outgoing page in the incoming
   * space's layout.
   */
  renderedPathname?: string | null;
  children: React.ReactNode;
}) {
  const { pathname, searchParams } = useNavigation();
  const sidebar = useSidebarSafe();
  const navSpace = spaceForPath(pathname);
  const space = renderedPathname ? spaceForPath(renderedPathname) : navSpace;
  const workspaceSlug = pathname.split("/").filter(Boolean)[0] ?? "";
  const search = searchParams?.toString() ?? "";
  // Keyed by `${workspaceSlug}/${space}`. The fragment is left out so a
  // comment deep link is not replayed on every return to the space.
  const [spaceUrls, setSpaceUrls] = useState<Record<string, string>>({});
  const hasPanel = space !== null && PANEL_SPACES.has(space);
  const [listCollapsed, setListCollapsed] = useState(false);
  const [peekSpace, setPeekSpace] = useState<AppSpaceId | null>(null);
  const openTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const closeTimer = useRef<ReturnType<typeof setTimeout> | null>(null);
  const peekRef = useRef(peekSpace);
  useEffect(() => {
    peekRef.current = peekSpace;
  }, [peekSpace]);

  const clearTimers = useCallback(() => {
    if (openTimer.current) clearTimeout(openTimer.current);
    if (closeTimer.current) clearTimeout(closeTimer.current);
    openTimer.current = null;
    closeTimer.current = null;
  }, []);

  const requestPeek = useCallback(
    (next: AppSpaceId) => {
      clearTimers();
      if (peekRef.current !== null) {
        setPeekSpace(next);
        return;
      }
      openTimer.current = setTimeout(() => setPeekSpace(next), PEEK_OPEN_DELAY_MS);
    },
    [clearTimers],
  );
  const holdPeek = useCallback(() => {
    if (closeTimer.current) clearTimeout(closeTimer.current);
    closeTimer.current = null;
  }, []);
  const releasePeek = useCallback(() => {
    clearTimers();
    closeTimer.current = setTimeout(() => setPeekSpace(null), PEEK_CLOSE_DELAY_MS);
  }, [clearTimers]);
  const closePeek = useCallback(() => {
    clearTimers();
    setPeekSpace(null);
  }, [clearTimers]);

  // Following a link out of the preview lands on its page; the preview has
  // done its job. Inbox and Chat select rows through the query string alone.
  const shownPathname = renderedPathname || pathname;
  useEffect(() => {
    closePeek();
  }, [shownPathname, search, closePeek]);
  useEffect(() => clearTimers, [clearTimers]);

  useEffect(() => {
    if (!navSpace || !workspaceSlug) return;
    const key = `${workspaceSlug}/${navSpace}`;
    const url = search ? `${pathname}?${search}` : pathname;
    setSpaceUrls((urls) => (urls[key] === url ? urls : { ...urls, [key]: url }));
  }, [pathname, search, navSpace, workspaceSlug]);

  const spaceHref = useCallback(
    (target: AppSpaceId, home: string) =>
      target === navSpace ? home : (spaceUrls[`${workspaceSlug}/${target}`] ?? home),
    [navSpace, spaceUrls, workspaceSlug],
  );

  const toggleSidebar = sidebar?.toggleSidebar;
  const setSidebarOpen = sidebar?.setOpen;
  const sidebarOpen = sidebar?.open ?? true;
  const isCompact = sidebar?.isCompact ?? false;
  const usesListColumn = space !== null && !hasPanel;
  // One toggle for the whole nav: whatever the current space shows decides
  // the direction, and the panel and every list column follow it together.
  const toggleNav = useCallback(() => {
    if (isCompact || !setSidebarOpen) {
      toggleSidebar?.();
      return;
    }
    const collapse = usesListColumn ? !listCollapsed : sidebarOpen;
    setSidebarOpen(!collapse);
    setListCollapsed(collapse);
  }, [isCompact, listCollapsed, setSidebarOpen, sidebarOpen, toggleSidebar, usesListColumn]);
  const navCollapsed = !isCompact && (usesListColumn ? listCollapsed : !sidebarOpen);

  const value = useMemo<AppShellContextValue>(
    () => ({
      space,
      hasPanel,
      listCollapsed,
      setListCollapsed,
      toggleNav,
      navCollapsed,
      spaceHref,
      peekSpace,
      requestPeek,
      holdPeek,
      releasePeek,
      closePeek,
    }),
    [
      space,
      hasPanel,
      listCollapsed,
      toggleNav,
      navCollapsed,
      spaceHref,
      peekSpace,
      requestPeek,
      holdPeek,
      releasePeek,
      closePeek,
    ],
  );

  return <AppShellContext.Provider value={value}>{children}</AppShellContext.Provider>;
}

/** Null outside the dashboard shell (standalone pages, isolated tests). */
export function useAppShell(): AppShellContextValue | null {
  return use(AppShellContext);
}

/**
 * Whether a list-and-detail page (Inbox, Chat, Settings) hides its list
 * column. Below `md` those pages manage the list themselves.
 */
export function useListColumnCollapsed(): boolean {
  const shell = useAppShell();
  const sidebar = useSidebarSafe();
  return Boolean(shell?.listCollapsed && !shell.hasPanel && !sidebar?.isCompact);
}
