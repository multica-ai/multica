import { useCallback } from "react";
import type { DataRouter } from "react-router-dom";
import { useActiveTabRouter, useActiveTabHistory } from "@/stores/tab-store";

/**
 * Shared hint map so useTabRouterSync can distinguish back vs forward POP.
 * Set before calling router.navigate(-1 | 1), read in the synchronous subscription.
 */
export const popDirectionHints = new Map<DataRouter, "back" | "forward">();

/**
 * Per-tab back/forward navigation derived from the active workspace's
 * active tab.
 *
 * Subscribed via primitive selectors so this hook only re-renders when
 * the numeric history state actually changes — path ticks on the active
 * tab (which don't shift historyIndex) don't churn the back/forward
 * buttons.
 */
export function useTabHistory() {
  const router = useActiveTabRouter();
  const { historyIndex, historyLength } = useActiveTabHistory();

  const canGoBack = historyIndex > 0;
  const canGoForward = historyIndex < historyLength - 1;

  const goBack = useCallback(() => {
    if (!router || historyIndex <= 0) return;
    popDirectionHints.set(router, "back");
    router.navigate(-1);
  }, [router, historyIndex]);

  const goForward = useCallback(() => {
    if (!router || historyIndex >= historyLength - 1) return;
    popDirectionHints.set(router, "forward");
    router.navigate(1);
  }, [router, historyIndex, historyLength]);

  return { canGoBack, canGoForward, goBack, goForward };
}
