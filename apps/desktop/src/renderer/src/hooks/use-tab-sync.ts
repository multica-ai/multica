import { useEffect } from "react";
import { useTabStore } from "@/stores/tab-store";

/**
 * Watches document.title via MutationObserver and updates the active tab's
 * title. Pages set document.title via TitleSync (route handle.title) or
 * useDocumentTitle(). This observer picks up the change and syncs it to
 * the tab store.
 */
export function useActiveTitleSync() {
  useEffect(() => {
    const observer = new MutationObserver(() => {
      const title = document.title;
      if (!title) return;
      const state = useTabStore.getState();
      if (!state.activeWorkspaceSlug) return;
      const group = state.byWorkspace[state.activeWorkspaceSlug];
      if (!group) return;
      const activeTab = group.tabs.find((t) => t.id === group.activeTabId);
      if (activeTab && activeTab.title !== title) {
        state.updateTab(activeTab.id, { title });
      }
    });

    const titleEl = document.querySelector("title");
    if (titleEl) {
      observer.observe(titleEl, { childList: true, characterData: true, subtree: true });
    }

    return () => observer.disconnect();
  }, []);
}
