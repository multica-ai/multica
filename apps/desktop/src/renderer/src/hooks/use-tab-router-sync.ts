import { useEffect, useRef } from "react";
import type { DataRouter } from "react-router-dom";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";
import { consumePendingDelta } from "@/platform/history-mirror";

/**
 * Subscribe to a tab's memory router and sync path + history tracking
 * back into the tab store.
 *
 * Called once per tab inside its RouterProvider subtree.
 */
export function useTabRouterSync(tabId: string, router: DataRouter) {
  const indexRef = useRef(0);
  const lengthRef = useRef(1);

  useEffect(() => {
    // Sync initial state
    const initialPath = router.state.location.pathname;
    const store = useTabStore.getState();
    store.updateTab(tabId, { path: initialPath, icon: resolveRouteIcon(initialPath) });

    const unsubscribe = router.subscribe((state) => {
      const { pathname } = state.location;
      const action = state.historyAction;

      if (action === "PUSH") {
        indexRef.current += 1;
        lengthRef.current = indexRef.current + 1;
      } else if (action === "POP") {
        // Move by the exact delta recorded by navigateByDelta — every POP in
        // the app is a self-initiated back/forward, so the delta is known.
        // Default to a single step back if one is somehow missing.
        const delta = consumePendingDelta(router) ?? -1;
        indexRef.current = Math.max(
          0,
          Math.min(indexRef.current + delta, lengthRef.current - 1),
        );
      }
      // REPLACE: index and length stay the same

      const store = useTabStore.getState();
      store.updateTab(tabId, { path: pathname, icon: resolveRouteIcon(pathname) });
      store.updateTabHistory(tabId, indexRef.current, lengthRef.current);
    });

    return unsubscribe;
  }, [tabId, router]);
}
