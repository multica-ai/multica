/**
 * Persistence for list view stores (issue and project filters).
 *
 * Mobile counterpart of web's `createWorkspaceAwareStorage` +
 * `registerForWorkspaceRehydration` (packages/core/platform/workspace-storage.ts):
 * keys are namespaced at call time by the active scope, and registered stores
 * reset and rehydrate when that scope changes, so each workspace keeps its own
 * view and a switch never carries filters across.
 *
 * The scope is user + workspace id rather than web's workspace slug.
 * SecureStore cannot enumerate keys, so sign-out cannot sweep them the way
 * web's session cleanup does; keying by user keeps the next account on this
 * device from reading the previous one's view. The id survives a slug rename.
 *
 * Backed by expo-secure-store, the persistence the binary already ships for
 * theme, locale and the last workspace, so no native module is added. A
 * snapshot is a few hundred bytes, below the ~2 KB value size some iOS
 * releases reject.
 */
import { useEffect } from "react";
import * as SecureStore from "expo-secure-store";
import type { StateStorage } from "zustand/middleware";

let activeScope: string | null = null;

// SecureStore accepts only [A-Za-z0-9._-] in keys.
function storageKey(name: string, scope: string): string {
  return `${name}.${scope}`.replace(/[^A-Za-z0-9._-]/g, "_");
}

/** Pass to `createJSONStorage`; persisted stores also set `skipHydration`. */
export const viewPrefsStorage: StateStorage = {
  // A failed read counts as nothing saved, so hydration still completes and
  // the store keeps its defaults.
  getItem: (name) =>
    activeScope
      ? SecureStore.getItemAsync(storageKey(name, activeScope)).catch(() => null)
      : null,
  // Without an active scope there is no owner for the value; this is also
  // what drops the writes caused by the reset in `setViewPrefsScope`.
  setItem: (name, value) => {
    if (!activeScope) return;
    return SecureStore.setItemAsync(storageKey(name, activeScope), value).catch(
      () => {
        // Keep the in-memory choice if secure storage is unavailable.
      },
    );
  },
  removeItem: (name) => {
    if (!activeScope) return;
    return SecureStore.deleteItemAsync(storageKey(name, activeScope)).catch(
      () => {},
    );
  },
};

interface PersistedViewStore<T> {
  getInitialState: () => T;
  setState: (state: T, replace: true) => void;
  persist: { rehydrate: () => unknown };
}

const registeredStores: { reset: () => void; rehydrate: () => unknown }[] = [];

/** Call once per store, next to its definition. */
export function registerViewPrefsStore<T>(store: PersistedViewStore<T>) {
  registeredStores.push({
    reset: () => store.setState(store.getInitialState(), true),
    rehydrate: () => store.persist.rehydrate(),
  });
}

/**
 * For stores that persist by hand rather than through `persist`: `reset` runs
 * on every scope change, before persisted stores rehydrate.
 */
export function onViewPrefsScopeChange(reset: () => void) {
  registeredStores.push({ reset, rehydrate: () => {} });
}

/**
 * Points every registered store at the given user's view of the given
 * workspace. Stores fall back to defaults first, so a workspace with nothing
 * saved never inherits the previous workspace's filters while its own
 * snapshot loads. `null` for either id detaches persistence.
 */
export function setViewPrefsScope(
  userId: string | null,
  workspaceId: string | null,
) {
  const next = userId && workspaceId ? `${userId}.${workspaceId}` : null;
  if (next === activeScope) return;
  activeScope = null;
  for (const store of registeredStores) store.reset();
  activeScope = next;
  if (!next) return;
  for (const store of registeredStores) void store.rehydrate();
}

/** Mounted once by the workspace layout, which owns the active workspace. */
export function useViewPrefsScope(
  userId: string | null,
  workspaceId: string | null,
) {
  useEffect(() => {
    setViewPrefsScope(userId, workspaceId);
  }, [userId, workspaceId]);
}
