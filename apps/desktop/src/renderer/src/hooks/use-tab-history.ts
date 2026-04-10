import { useCallback } from "react";
import type { DataRouter } from "react-router-dom";
import { useTabStore } from "@/stores/tab-store";

/**
 * Shared hint map so useTabRouterSync can distinguish back vs forward POP.
 * Set before calling router.navigate(-1 | 1), read in the synchronous subscription.
 */
export const popDirectionHints = new Map<DataRouter, "back" | "forward">();

/**
 * Per-tab back/forward navigation derived from the active tab's history state.
 * Replaces the old global useNavigationHistory() hook.
 */
export function useTabHistory() {
  // Return the actual tab object from the store — stable reference.
  // Do NOT create a new object in the selector (causes infinite re-renders).
  const activeTab = useTabStore((s) =>
    s.tabs.find((t) => t.id === s.activeTabId),
  );

  const canGoBack = (activeTab?.historyIndex ?? 0) > 0;
  const canGoForward =
    (activeTab?.historyIndex ?? 0) < (activeTab?.historyLength ?? 1) - 1;

  const goBack = useCallback(() => {
    if (!activeTab || activeTab.historyIndex <= 0) return;
    popDirectionHints.set(activeTab.router, "back");
    activeTab.router.navigate(-1);
  }, [activeTab]);

  const goForward = useCallback(() => {
    if (!activeTab || activeTab.historyIndex >= activeTab.historyLength - 1)
      return;
    popDirectionHints.set(activeTab.router, "forward");
    activeTab.router.navigate(1);
  }, [activeTab]);

  return { canGoBack, canGoForward, goBack, goForward };
}
