import { useSyncExternalStoreWithSelector } from "use-sync-external-store/with-selector.js";

export interface ExternalStateStore<State> {
  getSnapshot(): State;
  subscribe(listener: () => void): () => void;
}

/** React's selector binding for non-MobX host sources. */
export function useExternalStoreSelector<State, Selected>(
  store: ExternalStateStore<State>,
  selector: (state: State) => Selected,
  equals?: (left: Selected, right: Selected) => boolean
): Selected {
  return useSyncExternalStoreWithSelector(
    store.subscribe,
    store.getSnapshot,
    store.getSnapshot,
    selector,
    equals
  );
}
