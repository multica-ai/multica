"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  defaultStorage,
  registerForWorkspaceRehydration,
} from "@multica/core/platform";

export type ActorKey = `member:${string}` | `agent:${string}`;

export const actorKey = (
  type: "member" | "agent",
  id: string,
): ActorKey => `${type}:${id}` as ActorKey;

interface ChannelFavoritesState {
  favorites: ActorKey[];
  isFavorite: (key: ActorKey) => boolean;
  toggle: (key: ActorKey) => void;
}

/**
 * Per-workspace persisted list of favorited actors (members + agents) used
 * to pin frequent contacts at the top of the new-message picker. Backed by
 * the same workspace-aware localStorage adapter as other view stores so it
 * is namespaced by workspace slug and cleared on logout.
 */
export const useChannelFavoritesStore = create<ChannelFavoritesState>()(
  persist(
    (set, get) => ({
      favorites: [],
      isFavorite: (key) => get().favorites.includes(key),
      toggle: (key) =>
        set((state) => ({
          favorites: state.favorites.includes(key)
            ? state.favorites.filter((k) => k !== key)
            : [...state.favorites, key],
        })),
    }),
    {
      name: "multica_channel_favorites",
      storage: createJSONStorage(() => createWorkspaceAwareStorage(defaultStorage)),
    },
  ),
);

registerForWorkspaceRehydration(() => useChannelFavoritesStore.persist.rehydrate());
