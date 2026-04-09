import type { StorageAdapter } from "@multica/core/types/storage";

/**
 * SSR-safe localStorage wrapper.
 * Returns null / no-ops when running on the server (typeof window === "undefined").
 */
export const webStorage: StorageAdapter = {
  getItem: (k) =>
    typeof window !== "undefined" ? localStorage.getItem(k) : null,
  setItem: (k, v) => {
    if (typeof window !== "undefined") localStorage.setItem(k, v);
  },
  removeItem: (k) => {
    if (typeof window !== "undefined") localStorage.removeItem(k);
  },
};
