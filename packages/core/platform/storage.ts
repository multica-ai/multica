import type { StorageAdapter } from "../types/storage";

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) =>
    getLocalStorage()?.getItem(k) ?? null,
  setItem: (k, v) => {
    getLocalStorage()?.setItem(k, v);
  },
  removeItem: (k) => {
    getLocalStorage()?.removeItem(k);
  },
};

function getLocalStorage(): Storage | null {
  return typeof globalThis.localStorage !== "undefined"
    ? globalThis.localStorage
    : null;
}
