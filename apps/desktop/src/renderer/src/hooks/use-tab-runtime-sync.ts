import { useEffect } from "react";
import { useTabStore, resolveRouteIcon } from "@/stores/tab-store";
import type { TabRuntime } from "@/platform/tab-runtime";

/**
 * Feed a tab's live runtime (memory router + history mirror) back into the
 * store as its serializable session — on mount and on every committed
 * navigation.
 *
 * Replaces the old `use-tab-router-sync`: `path`/`icon` drive the tab bar and
 * dedupe, while the mirror snapshot is the authoritative full-location history
 * (search/hash included) that persistence and reload restore from. Guarded to
 * idle ticks so in-flight navigations aren't recorded.
 */
export function useTabRuntimeSync(tabId: string, runtime: TabRuntime): void {
  useEffect(() => {
    const sync = () => {
      const { pathname } = runtime.router.state.location;
      useTabStore.getState().syncTabRuntime(tabId, {
        path: pathname,
        icon: resolveRouteIcon(pathname),
        session: runtime.mirror.snapshot(),
      });
    };
    sync();
    return runtime.router.subscribe((state) => {
      if (state.navigation.state === "idle") sync();
    });
  }, [tabId, runtime]);
}
