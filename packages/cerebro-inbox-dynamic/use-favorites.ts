// TECH-3579 — favorite conversations in the dynamic inbox.
//
// A favorite is a personal, inbox-only star: it floats a conversation to the
// top of the "All messages" box and feeds the standalone Favorites block. It is
// deliberately separate from the sidebar "Pin" (which also pins to the sidebar
// and has no representation for agent chats). Favorites live in the same
// server-synced user.preferences blob the inbox layout uses (PATCH
// /api/me/preferences via setUser), so they follow the user across devices with
// no extra table or endpoint.
"use client";

import { useCallback, useMemo, useState } from "react";
import { useAuthStore } from "@multica/core/auth";
import { api } from "@multica/core/api";
import type { DynInboxEntry } from "./section-filter";

export const INBOX_FAVORITES_KEY = "cerebro_inbox_favorites";

/**
 * Stable key for the conversation behind an inbox entry. A notif's conversation
 * is its issue (so every notification for the same issue shares one favorite);
 * chats and channels key on their own id. The `item.id` fallback keeps a
 * conversation-less notification still toggleable.
 */
export function favoriteKeyForEntry(entry: DynInboxEntry): string {
  switch (entry.kind) {
    case "notif":
      return `issue:${entry.item.issue_id ?? entry.item.id}`;
    case "chat":
      return `chat:${entry.session.id}`;
    case "channel":
      return `channel:${entry.channel.id}`;
    default:
      return "";
  }
}

/** Defensive: read the favorites list from an untrusted preferences blob. */
export function readFavorites(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((v): v is string => typeof v === "string" && v.length > 0);
}

/** Pure toggle: add the key if absent, remove it if present. */
export function toggleFavoriteKey(keys: readonly string[], key: string): string[] {
  if (!key) return [...keys];
  return keys.includes(key) ? keys.filter((k) => k !== key) : [...keys, key];
}

export interface UseInboxFavoritesResult {
  /** Set of favorited conversation keys (stable reference per render). */
  favoriteKeys: ReadonlySet<string>;
  /** Is this entry's conversation a favorite right now? */
  isFavorite: (entry: DynInboxEntry) => boolean;
  /** Star / unstar the entry's conversation (optimistic, server-synced). */
  toggleFavorite: (entry: DynInboxEntry) => void;
}

export function useInboxFavorites(): UseInboxFavoritesResult {
  const user = useAuthStore((s) => s.user);
  const setUser = useAuthStore((s) => s.setUser);

  const prefs = user?.preferences as Record<string, unknown> | null | undefined;
  const saved = useMemo(() => readFavorites(prefs?.[INBOX_FAVORITES_KEY]), [prefs]);

  // Optimistic mirror so a star toggles instantly; seeded from prefs and
  // rolled back to server truth on a failed write.
  const [optimistic, setOptimistic] = useState<string[] | null>(null);
  const favorites = optimistic ?? saved;
  const favoriteKeys = useMemo(() => new Set(favorites), [favorites]);

  const isFavorite = useCallback(
    (entry: DynInboxEntry) => favoriteKeys.has(favoriteKeyForEntry(entry)),
    [favoriteKeys],
  );

  const toggleFavorite = useCallback(
    (entry: DynInboxEntry) => {
      const key = favoriteKeyForEntry(entry);
      if (!key) return;
      const next = toggleFavoriteKey(favorites, key);
      setOptimistic(next);
      void (async () => {
        try {
          const updated = await api.updateMyPreferences({ [INBOX_FAVORITES_KEY]: next });
          setUser(updated);
          setOptimistic(null);
        } catch {
          // Roll back to the server truth on failure.
          setOptimistic(null);
        }
      })();
    },
    [favorites, setUser],
  );

  return { favoriteKeys, isFavorite, toggleFavorite };
}
