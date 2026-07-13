import { useCallback } from "react";
import { useActiveTabHistory } from "@/stores/tab-store";
import { useActiveTabRouter } from "@/platform/tab-runtime";
import { navigateByDelta } from "@/platform/history-mirror";

/**
 * Per-tab back/forward navigation derived from the active workspace's
 * active tab.
 *
 * `canGoBack` / `canGoForward` come from the active tab's persisted history
 * session (via `useActiveTabHistory`), so they update only on real
 * navigations. The live router comes from the runtime registry
 * (`useActiveTabRouter`). Relative navigation goes through the single
 * `navigateByDelta` helper so the history mirror records the exact delta.
 */
export function useTabHistory() {
  const router = useActiveTabRouter();
  const { canGoBack, canGoForward } = useActiveTabHistory();

  const goBack = useCallback(() => {
    if (!router || !canGoBack) return;
    void navigateByDelta(router, -1);
  }, [router, canGoBack]);

  const goForward = useCallback(() => {
    if (!router || !canGoForward) return;
    void navigateByDelta(router, 1);
  }, [router, canGoForward]);

  return { canGoBack, canGoForward, goBack, goForward };
}
