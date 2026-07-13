import { useCallback } from "react";
import { useActiveTabRouter, useActiveTabHistory } from "@/stores/tab-store";
import { navigateByDelta } from "@/platform/history-mirror";

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
    void navigateByDelta(router, -1);
  }, [router, historyIndex]);

  const goForward = useCallback(() => {
    if (!router || historyIndex >= historyLength - 1) return;
    void navigateByDelta(router, 1);
  }, [router, historyIndex, historyLength]);

  return { canGoBack, canGoForward, goBack, goForward };
}
