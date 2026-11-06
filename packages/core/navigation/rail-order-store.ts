"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { RAIL_SPACES, type RailSpaceId } from "../paths";

/**
 * Resolve a saved rail order against the space registry. Unknown ids (a space
 * removed since the order was saved) and repeats are dropped; spaces the saved
 * order never saw keep their registry slot after the ordered ones, so a rail
 * shipped after the preference never disappears from it. An unusable order
 * falls back to the registry order.
 */
export function orderedRailSpaces(order: unknown): RailSpaceId[] {
  const seen = new Set<RailSpaceId>();
  if (Array.isArray(order)) {
    for (const id of order) {
      if ((RAIL_SPACES as readonly string[]).includes(id as string)) {
        seen.add(id as RailSpaceId);
      }
    }
  }
  return [...seen, ...RAIL_SPACES.filter((id) => !seen.has(id))];
}

interface RailOrderState {
  /** The rail's display order; always a full, valid space list. */
  order: RailSpaceId[];
  setRailOrder: (order: RailSpaceId[]) => void;
}

/**
 * The user's app-rail order. A device-level layout preference like keyboard
 * shortcuts, not per-workspace state: the rail lists the same spaces in every
 * workspace, and muscle memory should not diverge between them.
 */
export const useRailOrderStore = create<RailOrderState>()(
  persist(
    (set) => ({
      order: [...RAIL_SPACES],
      setRailOrder: (order) => set({ order }),
    }),
    {
      name: "multica_rail_order",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ order: state.order }),
      // Storage only ever sees the setter's validated lists, but merge still
      // resolves through the registry so hand-edited or stale storage stays
      // renderable.
      merge: (persisted, current) => ({
        ...current,
        order: orderedRailSpaces((persisted as { order?: unknown } | undefined)?.order),
      }),
    },
  ),
);
