import type { StorageAdapter } from "../types/storage";

const browserStorage: StorageAdapter = {
  getItem: (k) =>
    getLocalStorage()?.getItem(k) ?? null,
  setItem: (k, v) => {
    getLocalStorage()?.setItem(k, v);
  },
  removeItem: (k) => {
    getLocalStorage()?.removeItem(k);
  },
};

let configuredStorage: StorageAdapter | null = null;

export function setDefaultStorageAdapter(storage: StorageAdapter) {
  configuredStorage = storage;
}

/** SSR-safe localStorage. Works in both Next.js (SSR) and Electron (always client). */
export const defaultStorage: StorageAdapter = {
  getItem: (k) =>
    (configuredStorage ?? browserStorage).getItem(k),
  setItem: (k, v) => {
    (configuredStorage ?? browserStorage).setItem(k, v);
  },
  removeItem: (k) => {
    (configuredStorage ?? browserStorage).removeItem(k);
  },
};

function getLocalStorage(): Storage | null {
  return typeof globalThis.localStorage !== "undefined"
    ? globalThis.localStorage
    : null;
}
