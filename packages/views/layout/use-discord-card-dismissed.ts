"use client";

import { useCallback, useSyncExternalStore } from "react";

/**
 * Per-user "I dismissed the Join Discord sidebar card" flag.
 *
 * Lives in `localStorage` rather than the server because it is a purely
 * frontend UX preference (don't show this promo again) that never feeds
 * analytics or the backend. Keyed by user id so two people sharing a
 * browser profile don't inherit each other's dismissal.
 *
 * Tradeoff: clearing browser storage re-shows the card, and the flag does
 * not sync across devices. Both are acceptable for a low-stakes promo.
 */

const STORAGE_PREFIX = "multica.discord_card.dismissed.";

function storageKey(userId: string): string {
  return `${STORAGE_PREFIX}${userId}`;
}

function readDismissed(userId: string | null | undefined): boolean {
  if (!userId) return false;
  if (typeof window === "undefined") return false;
  try {
    return window.localStorage.getItem(storageKey(userId)) === "1";
  } catch {
    return false;
  }
}

// Shared listener registry so a same-tab write wakes every subscribed hook.
const listeners = new Set<() => void>();
function notify(): void {
  for (const fn of listeners) fn();
}

function subscribe(callback: () => void): () => void {
  listeners.add(callback);
  // Cross-tab updates arrive via the storage event.
  const handler =
    typeof window === "undefined"
      ? undefined
      : (e: StorageEvent) => {
          if (e.key && e.key.startsWith(STORAGE_PREFIX)) callback();
        };
  if (handler) window.addEventListener("storage", handler);
  return () => {
    listeners.delete(callback);
    if (handler) window.removeEventListener("storage", handler);
  };
}

/** Returns `[dismissed, dismiss]`. `dismiss` is a no-op without a user id. */
export function useDiscordCardDismissed(
  userId: string | null | undefined,
): readonly [boolean, () => void] {
  const dismissed = useSyncExternalStore(
    subscribe,
    () => readDismissed(userId),
    () => false,
  );

  const dismiss = useCallback(() => {
    if (!userId) return;
    if (typeof window === "undefined") return;
    try {
      window.localStorage.setItem(storageKey(userId), "1");
      notify();
    } catch {
      // localStorage can throw in private-mode Safari and similar — treat
      // as a no-op. Worst case the card re-shows on the next mount.
    }
  }, [userId]);

  return [dismissed, dismiss] as const;
}
