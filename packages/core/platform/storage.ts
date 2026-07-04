import type { StorageAdapter } from "../types/storage";

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) =>
    typeof window !== "undefined" && window.localStorage ? localStorage.getItem(k) : null,
  setItem: (k, v) => {
    if (typeof window !== "undefined" && window.localStorage) localStorage.setItem(k, v);
  },
  removeItem: (k) => {
    if (typeof window !== "undefined" && window.localStorage) localStorage.removeItem(k);
  },
};
