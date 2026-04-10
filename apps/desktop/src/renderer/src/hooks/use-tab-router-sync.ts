import { useEffect, useRef } from "react";
import type { DataRouter } from "react-router-dom";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";
import { popDirectionHints } from "./use-tab-history";

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
        // Determine direction from the hint set by goBack/goForward
        const hint = popDirectionHints.get(router);
        popDirectionHints.delete(router);
        if (hint === "forward") {
          indexRef.current = Math.min(indexRef.current + 1, lengthRef.current - 1);
        } else {
          // Default to back
          indexRef.current = Math.max(0, indexRef.current - 1);
        }
      }
      // REPLACE: index and length stay the same

      const store = useTabStore.getState();
      store.updateTab(tabId, { path: pathname, icon: resolveRouteIcon(pathname) });
      store.updateTabHistory(tabId, indexRef.current, lengthRef.current);
    });

    return unsubscribe;
  }, [tabId, router]);
}
