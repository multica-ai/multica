import type { StateStorage } from "zustand/middleware";
import type { StorageAdapter } from "../types/storage";

/**
 * Bridge between Zustand persist middleware and our StorageAdapter DI system.
 * For workspace-scoped stores, use createWorkspaceAwareStorage instead.
 */
export function createPersistStorage(adapter: StorageAdapter): StateStorage {
  return {
    getItem: (key) => adapter.getItem(key),
    setItem: (key, value) => adapter.setItem(key, value),
    removeItem: (key) => adapter.removeItem(key),
  };
}
