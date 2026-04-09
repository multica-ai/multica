export { createAuthStore } from "./store";
export type { AuthStoreOptions, AuthState } from "./store";

import type { createAuthStore as CreateAuthStoreFn } from "./store";

type AuthStoreInstance = ReturnType<typeof CreateAuthStoreFn>;

/** Module-level singleton — set once at app boot via `registerAuthStore()`. */
let _store: AuthStoreInstance | null = null;

/**
 * Register the auth store instance created by the app.
 * Must be called at boot before any component renders.
 */
export function registerAuthStore(store: AuthStoreInstance) {
  _store = store;
}

/**
 * Singleton accessor — a Zustand hook backed by the registered instance.
 * Supports `useAuthStore(selector)` and `useAuthStore.getState()`.
 */
export const useAuthStore: AuthStoreInstance = new Proxy(
  (() => {}) as unknown as AuthStoreInstance,
  {
    apply(_target, _thisArg, args) {
      if (!_store)
        throw new Error(
          "Auth store not initialised — call registerAuthStore() first",
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
