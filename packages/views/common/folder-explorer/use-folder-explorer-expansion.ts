"use client";

import { useCallback, useMemo, useSyncExternalStore } from "react";

const STORAGE_PREFIX = "multica.folder_explorer.expanded.";
const listeners = new Set<() => void>();
const memoryFallback = new Map<string, string>();

export function folderExplorerStorageKey(namespace: string, wsId: string): string {
  return `${STORAGE_PREFIX}${namespace}.${wsId}`;
}

function parseExpandedIds(raw: string | null): ReadonlySet<string> {
  if (raw === null) return new Set();
  try {
    const value: unknown = JSON.parse(raw);
    if (!Array.isArray(value)) return new Set();
    return new Set(value.filter((item): item is string => typeof item === "string"));
  } catch {
    return new Set();
  }
}

function readRaw(key: string): string | null {
  if (typeof window === "undefined") return null;
  const fallback = memoryFallback.get(key);
  if (fallback !== undefined) return fallback;
  try {
    return window.localStorage.getItem(key);
  } catch {
    return null;
  }
}

function notify(): void {
  for (const listener of listeners) listener();
}

function subscribe(callback: () => void): () => void {
  listeners.add(callback);
  const onStorage = (event: StorageEvent) => {
    if (event.key?.startsWith(STORAGE_PREFIX)) {
      memoryFallback.delete(event.key);
      callback();
    }
  };
  if (typeof window !== "undefined") window.addEventListener("storage", onStorage);
  return () => {
    listeners.delete(callback);
    if (typeof window !== "undefined") window.removeEventListener("storage", onStorage);
  };
}

function writeExpandedIds(key: string, expandedIds: ReadonlySet<string>): void {
  const raw = JSON.stringify([...expandedIds]);
  if (typeof window !== "undefined") {
    try {
      window.localStorage.setItem(key, raw);
      memoryFallback.delete(key);
    } catch {
      memoryFallback.set(key, raw);
    }
  } else {
    memoryFallback.set(key, raw);
  }
  notify();
}

export function useFolderExplorerExpansion(namespace: string, wsId: string) {
  const key = folderExplorerStorageKey(namespace, wsId);
  const raw = useSyncExternalStore(
    subscribe,
    () => readRaw(key),
    () => null,
  );
  const expandedFolderIds = useMemo(() => parseExpandedIds(raw), [raw]);

  const toggleFolder = useCallback((folderId: string) => {
    const next = new Set(parseExpandedIds(readRaw(key)));
    if (next.has(folderId)) next.delete(folderId);
    else next.add(folderId);
    writeExpandedIds(key, next);
  }, [key]);

  const expandFolder = useCallback((folderId: string) => {
    const next = new Set(parseExpandedIds(readRaw(key)));
    next.add(folderId);
    writeExpandedIds(key, next);
  }, [key]);

  const expandAll = useCallback((folderIds: readonly string[]) => {
    writeExpandedIds(key, new Set(folderIds));
  }, [key]);

  const collapseAll = useCallback(() => {
    writeExpandedIds(key, new Set());
  }, [key]);

  return {
    expandedFolderIds,
    toggleFolder,
    expandFolder,
    expandAll,
    collapseAll,
  };
}
