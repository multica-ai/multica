import { useEffect } from "react";
import { useTabStore } from "@/stores/tab-store";

/**
 * Watches document.title via MutationObserver and updates the active tab's title.
 *
 * Pages set document.title via TitleSync (route handle.title) or useDocumentTitle().
 * This observer picks up the change and syncs it to the tab store.
 */
export function useActiveTitleSync() {
  useEffect(() => {
    const observer = new MutationObserver(() => {
      const title = document.title;
      if (!title) return;
      const { tabs, activeTabId } = useTabStore.getState();
      const activeTab = tabs.find((t) => t.id === activeTabId);
      if (activeTab && activeTab.title !== title) {
        useTabStore.getState().updateTab(activeTabId, { title });
      }
    });

    const titleEl = document.querySelector("title");
    if (titleEl) {
      observer.observe(titleEl, { childList: true, characterData: true, subtree: true });
    }

    return () => observer.disconnect();
  }, []);
}
