import type { StorageAdapter } from "../types/storage";

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
function getLocalStorage(): Storage | null {
  if (typeof window === "undefined") return null;
  return window.localStorage ?? null;
}

export const defaultStorage: StorageAdapter = {
  getItem: (k) => getLocalStorage()?.getItem(k) ?? null,
  setItem: (k, v) => {
    getLocalStorage()?.setItem(k, v);
  },
  removeItem: (k) => {
    getLocalStorage()?.removeItem(k);
  },
};
