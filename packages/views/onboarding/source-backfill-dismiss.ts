"use client";

import { useCallback, useSyncExternalStore } from "react";

/**
 * Per-user dismiss-count storage for the source-backfill prompt.
 *
 * Lives in `localStorage` rather than the server because:
 *   - It is purely a frontend UX concern (when to stop nagging) and
 *     never feeds analytics or the backend.
 *   - Counting in the DB would mean writing on every page view, which
 *     is wasteful for a transient signal.
 *
 * Tradeoff: clearing browser storage resets the count, and the count
 * does not sync across devices. Both are acceptable — Submit and Skip
 * are the terminal paths that write to the server; dismiss is "ask me
 * later," and asking again on a fresh browser is fine.
 */

const STORAGE_PREFIX = "multica.source_backfill.dismiss.";

function storageKey(userId: string): string {
  return `${STORAGE_PREFIX}${userId}`;
}

function readCount(userId: string | null | undefined): number {
  if (!userId) return 0;
  if (typeof window === "undefined") return 0;
  try {
    const raw = window.localStorage.getItem(storageKey(userId));
    if (!raw) return 0;
    const parsed = Number.parseInt(raw, 10);
    return Number.isFinite(parsed) && parsed >= 0 ? parsed : 0;
  } catch {
    return 0;
  }
}

function subscribe(callback: () => void): () => void {
  if (typeof window === "undefined") return () => {};
  // Fires when another tab updates the value. Same-tab updates go
  // through `forceUpdate` below.
  const handler = (e: StorageEvent) => {
    if (e.key && e.key.startsWith(STORAGE_PREFIX)) callback();
  };
  window.addEventListener("storage", handler);
  return () => window.removeEventListener("storage", handler);
}

// Shared listener registry so `bumpDismissCount` outside the React tree
// (rare, but possible) still wakes hooks subscribed to the same key.
const listeners = new Set<() => void>();
function notify(): void {
  for (const fn of listeners) fn();
}

export function useSourceBackfillDismissCount(
  userId: string | null | undefined,
): readonly [number, () => void] {
  const count = useSyncExternalStore(
    (cb) => {
      listeners.add(cb);
      const off = subscribe(cb);
      return () => {
        listeners.delete(cb);
        off();
      };
    },
    () => readCount(userId),
    () => 0,
  );

  const bump = useCallback(() => {
    if (!userId) return;
    if (typeof window === "undefined") return;
    try {
      const next = readCount(userId) + 1;
      window.localStorage.setItem(storageKey(userId), String(next));
      notify();
    } catch {
      // localStorage write can fail in private-mode Safari and similar
      // — treat as a no-op. Worst case the prompt re-shows next login
      // without backing off.
    }
  }, [userId]);

  return [count, bump] as const;
}

/** Test helper — not exported from the package index. */
export function _resetSourceBackfillDismissCountForTests(): void {
  if (typeof window === "undefined") return;
  for (let i = window.localStorage.length - 1; i >= 0; i--) {
    const k = window.localStorage.key(i);
    if (k && k.startsWith(STORAGE_PREFIX)) window.localStorage.removeItem(k);
  }
  notify();
}
