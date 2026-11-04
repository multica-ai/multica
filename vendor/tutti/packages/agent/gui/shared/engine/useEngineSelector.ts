import { reaction } from "mobx";
import { useCallback, useSyncExternalStore } from "react";
import { useSyncExternalStoreWithSelector } from "use-sync-external-store/with-selector.js";

// Single React binding for workspace session engine subscriptions
// (docs/architecture/agent-gui-refactor-plan.md, sections 3.5 and 4.1).
//
// This is the only file in the package allowed to touch useSyncExternalStore
// (enforced by tools/scripts/check-agent-gui-degradation.mjs). Components
// subscribe through useEngineSelector so selector memoization and equality
// comparison are implemented exactly once, via React's official with-selector
// shim; hand-rolled subscriptions returning fresh references are the classic
// infinite re-render trap this file exists to contain.

/**
 * Structural surface the binding needs from an engine instance. Matches
 * AgentSessionEngine from @tutti-os/agent-activity-core, but stays structural
 * so focused tests and future engine slices can bind without the full engine.
 *
 * Contract: `subscribe` and `getSnapshot` must be stable, this-free function
 * references for the lifetime of the instance (the engine factory returns
 * closures, satisfying this by construction). A fresh `subscribe` identity per
 * render would force useSyncExternalStore to resubscribe on every render.
 */
export interface EngineStateStore<TState> {
  getSnapshot(): TState;
  subscribe(listener: () => void): () => void;
}

export function useEngineSelector<TState, TSelected>(
  engine: EngineStateStore<TState>,
  selector: (state: TState) => TSelected,
  isEqual?: (a: TSelected, b: TSelected) => boolean
): TSelected {
  return useSyncExternalStoreWithSelector(
    engine.subscribe,
    engine.getSnapshot,
    engine.getSnapshot,
    selector,
    isEqual
  );
}

/**
 * Reads MobX state from a hook outside an observer component, while controllers
 * still pass values down. `deps` are what `read` closes over.
 */
export function useModelValue<T>(read: () => T, deps: readonly unknown[]): T {
  const subscribe = useCallback(
    (listener: () => void) => reaction(read, listener),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    deps
  );
  return useSyncExternalStore(subscribe, read, read);
}
