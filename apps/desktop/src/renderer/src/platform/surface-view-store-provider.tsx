import { useCallback, useRef, type ReactNode } from "react";
import { createStore, type StoreApi } from "zustand/vanilla";
import {
  IssueViewStoreFactoryProvider,
  viewStoreSlice,
  mergeViewStatePersisted,
  partializeIssueViewState,
  type IssueViewState,
  type IssueViewStoreFactory,
} from "@multica/core/issues/stores";
import { useTabStore, getTabById } from "@/stores/tab-store";

/**
 * Build a view store whose state is owned by the tab session (MUL-4475
 * blocker 2). It hydrates ONCE from `tab.viewState[surfaceKey]` at creation and
 * thereafter only writes its own changes back via `updateTabViewState`. It
 * never re-hydrates from the tab store, so a write-back can't feed back and
 * reset it (Howard risk 3).
 */
function createSessionBackedViewStore(
  tabId: string,
  surfaceKey: string,
): StoreApi<IssueViewState> {
  const store = createStore<IssueViewState>()((set) => viewStoreSlice(set));
  const persisted = getTabById(useTabStore.getState(), tabId)?.viewState?.[
    surfaceKey
  ];
  const defaults = viewStoreSlice(store.setState);
  // Hydrate before subscribing so the seeding set() isn't counted as a change.
  store.setState(mergeViewStatePersisted(persisted, defaults), true);
  store.subscribe((state) => {
    useTabStore
      .getState()
      .updateTabViewState(tabId, surfaceKey, partializeIssueViewState(state));
  });
  return store;
}

/**
 * Desktop-only: injects a per-tab, session-backed view-store factory so each
 * tab's IssueSurface keeps its own filters/sort/viewMode — owned by the tab
 * session, released when the tab closes — instead of sharing the global
 * surface registry. Mounted inside each tab's subtree, closing over the tab id.
 */
export function SurfaceViewStoreProvider({
  tabId,
  children,
}: {
  tabId: string;
  children: ReactNode;
}) {
  // One store per surfaceKey, cached in a ref so tab-store write-backs (which
  // re-render this provider) never rebuild the factory or reset a store. The
  // factory identity is stable across renders (deps only on tabId), so the
  // consuming IssueSurface never re-resolves its store either.
  const cacheRef = useRef<Map<string, StoreApi<IssueViewState>>>(new Map());
  const factory: IssueViewStoreFactory = useCallback(
    (surfaceKey) => {
      const cache = cacheRef.current;
      const existing = cache.get(surfaceKey);
      if (existing) return existing;
      const store = createSessionBackedViewStore(tabId, surfaceKey);
      cache.set(surfaceKey, store);
      return store;
    },
    [tabId],
  );

  return (
    <IssueViewStoreFactoryProvider factory={factory}>
      {children}
    </IssueViewStoreFactoryProvider>
  );
}
