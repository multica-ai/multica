import { useEffect, useRef } from "react";
import { useLocation } from "react-router-dom";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";

/**
 * Keeps the active tab in sync with the current URL and document.title.
 *
 * Two sync directions:
 * 1. URL change → update active tab's path (and set a default title from the route)
 * 2. document.title change → update active tab's title (pages set this naturally)
 *
 * Tab switches (clicking another tab) are ignored — the tab already has its metadata.
 */
export function useTabSync() {
  const location = useLocation();
  const isTabSwitch = useRef(false);

  // Detect tab switches so we don't overwrite metadata
  useEffect(() => {
    return useTabStore.subscribe((state, prev) => {
      if (state.activeTabId !== prev.activeTabId) {
        isTabSwitch.current = true;
      }
    });
  }, []);

  // Sync URL → tab path
  useEffect(() => {
    if (isTabSwitch.current) {
      isTabSwitch.current = false;
      return;
    }

    const { tabs, activeTabId } = useTabStore.getState();
    const activeTab = tabs.find((t) => t.id === activeTabId);
    if (activeTab && activeTab.path !== location.pathname) {
      const icon = resolveRouteIcon(location.pathname);
      useTabStore.getState().updateActiveTab(location.pathname, document.title, icon);
    }
  }, [location.pathname]);

  // Sync document.title → tab title
  useEffect(() => {
    const observer = new MutationObserver(() => {
      const title = document.title;
      if (!title) return;
      const { tabs, activeTabId } = useTabStore.getState();
      const activeTab = tabs.find((t) => t.id === activeTabId);
      if (activeTab && activeTab.title !== title) {
        useTabStore.getState().updateActiveTab(activeTab.path, title, activeTab.icon);
      }
    });

    const titleEl = document.querySelector("title");
    if (titleEl) {
      observer.observe(titleEl, { childList: true, characterData: true, subtree: true });
    }

    return () => observer.disconnect();
  }, []);
}
