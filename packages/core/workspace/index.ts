export * from "./store";
export * from "./queries";
export * from "./mutations";
export * from "./hooks";

import type { createWorkspaceStore as CreateWorkspaceStoreFn } from "./store";

type WorkspaceStoreInstance = ReturnType<typeof CreateWorkspaceStoreFn>;

/** Module-level singleton — set once at app boot via `registerWorkspaceStore()`. */
let _store: WorkspaceStoreInstance | null = null;

/**
 * Register the workspace store instance created by the app.
 * Must be called at boot before any component renders.
 */
export function registerWorkspaceStore(store: WorkspaceStoreInstance) {
  _store = store;
}

/**
 * Singleton accessor — a Zustand hook backed by the registered instance.
 * Supports `useWorkspaceStore(selector)` and `useWorkspaceStore.getState()`.
 */
export const useWorkspaceStore: WorkspaceStoreInstance = new Proxy(
  (() => {}) as unknown as WorkspaceStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store)
        throw new Error(
          "Workspace store not initialised — call registerWorkspaceStore() first",
        );
      return (_store as unknown as (...a: unknown[]) => unknown)(...args);
    },
    get(_target, prop) {
      // Allow property inspection (HMR/React Refresh) before registration
      if (!_store) return undefined;
      return Reflect.get(_store, prop);
    },
  },
);
