import type { StorageAdapter } from "../types/storage";

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) =>
    typeof window !== "undefined" ? localStorage.getItem(k) : null,
  setItem: (k, v) => {
    if (typeof window !== "undefined") localStorage.setItem(k, v);
  },
  removeItem: (k) => {
    if (typeof window !== "undefined") localStorage.removeItem(k);
  },
  keys: () => {
    if (typeof window === "undefined") return [];
    return Object.keys(localStorage);
  },
  subscribe: (listener) => {
    if (typeof window === "undefined") return () => {};
    // Subscribers re-read through getItem, so an event for another storage
    // area costs a redundant read at most.
    const onStorage = (event: StorageEvent) => listener(event.key);
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  },
  update: async (key, updater) => {
    if (typeof window === "undefined") return;
    const run = () => {
      const next = updater(localStorage.getItem(key));
      if (next !== null) localStorage.setItem(key, next);
    };
    // localStorage itself gives no cross-tab atomicity: two renderer
    // processes can each read before either writes. A Web Lock named after
    // the key serializes every update to it across the origin. Without Web
    // Locks (very old browsers) the read and write still run back to back.
    const locks = typeof navigator !== "undefined" ? navigator.locks : undefined;
    if (!locks) {
      run();
      return;
    }
    await locks.request(`multica_storage:${key}`, run);
  },
};
