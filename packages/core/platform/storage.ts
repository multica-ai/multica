import type { StorageAdapter } from "../types/storage";

let backend: Required<StorageAdapter> | null = null;

/**
 * Routes `defaultStorage` to a host-provided store, for hosts without Web
 * Storage (React Native). Persisted stores hydrate when their module is first
 * evaluated, so this must run before anything imports them. The store must
 * list its keys: session cleanup relies on that to erase every workspace's data.
 */
export function installDefaultStorageBackend(adapter: Required<StorageAdapter>): void {
  backend = adapter;
}

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) => {
    if (backend) return backend.getItem(k);
    return typeof window !== "undefined" ? localStorage.getItem(k) : null;
  },
  setItem: (k, v) => {
    if (backend) backend.setItem(k, v);
    else if (typeof window !== "undefined") localStorage.setItem(k, v);
  },
  removeItem: (k) => {
    if (backend) backend.removeItem(k);
    else if (typeof window !== "undefined") localStorage.removeItem(k);
  },
  keys: () => {
    if (backend) return backend.keys();
    if (typeof window === "undefined") return [];
    return Object.keys(localStorage);
  },
};
