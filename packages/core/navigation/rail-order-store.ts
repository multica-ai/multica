"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { RAIL_SPACES, type RailSpaceId } from "../paths/spaces";
import { defaultStorage } from "../platform/storage";

const RAIL_SPACE_IDS = new Set<string>(RAIL_SPACES);

/**
 * The rail's spaces in the user's order. Unknown and repeated ids are
 * dropped; a space missing from the saved order (one added since it was
 * saved) is placed right after the space it follows by default.
 */
export function resolveRailOrder(saved: readonly unknown[] | null): RailSpaceId[] {
  if (!saved) return [...RAIL_SPACES];
  const order = [...new Set(saved)].filter(
    (id): id is RailSpaceId => typeof id === "string" && RAIL_SPACE_IDS.has(id),
  );
  RAIL_SPACES.forEach((id, i) => {
    if (order.includes(id)) return;
    const previous = i === 0 ? -1 : order.indexOf(RAIL_SPACES[i - 1]!);
    order.splice(previous + 1, 0, id);
  });
  return order;
}

interface RailOrderState {
  /** `null` until the user reorders, so the default order can still evolve. */
  order: RailSpaceId[] | null;
  setOrder: (order: readonly RailSpaceId[]) => void;
}

/** This device's rail order, shared by every workspace. */
export const useRailOrderStore = create<RailOrderState>()(
  persist(
    (set) => ({
      order: null,
      setOrder: (next) => {
        const order = resolveRailOrder(next);
        const isDefault = order.every((id, i) => id === RAIL_SPACES[i]);
        set({ order: isDefault ? null : order });
      },
    }),
    {
      name: "multica_rail_order",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ order: state.order }),
      merge: (persisted, current) => {
        const saved = (persisted as { order?: unknown } | undefined)?.order;
        return { ...current, order: Array.isArray(saved) ? resolveRailOrder(saved) : null };
      },
    },
  ),
);
