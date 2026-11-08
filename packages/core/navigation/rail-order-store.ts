"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { RAIL_SPACES, type RailSpaceId } from "../paths/spaces";
import { defaultStorage } from "../platform/storage";

/** App-wide actions that sit in the rail beside the spaces. */
export type RailActionId = "new_issue" | "search";
export type RailItemId = RailActionId | RailSpaceId;

export const DEFAULT_RAIL_ORDER: readonly RailItemId[] = ["new_issue", "search", ...RAIL_SPACES];

const RAIL_ITEM_IDS = new Set<string>(DEFAULT_RAIL_ORDER);

/**
 * The rail in the user's order. Unknown and repeated ids are dropped; an item
 * missing from the saved order (one added since it was saved) keeps its
 * default neighbour, placed right after the item it follows by default.
 */
export function resolveRailOrder(saved: readonly unknown[] | null): RailItemId[] {
  if (!saved) return [...DEFAULT_RAIL_ORDER];
  const order = [...new Set(saved)].filter(
    (id): id is RailItemId => typeof id === "string" && RAIL_ITEM_IDS.has(id),
  );
  DEFAULT_RAIL_ORDER.forEach((id, i) => {
    if (order.includes(id)) return;
    const previous = i === 0 ? -1 : order.indexOf(DEFAULT_RAIL_ORDER[i - 1]!);
    order.splice(previous + 1, 0, id);
  });
  return order;
}

interface RailOrderState {
  /** `null` until the user reorders, so the default order can still evolve. */
  order: RailItemId[] | null;
  setOrder: (order: readonly RailItemId[]) => void;
}

/** This device's rail order, shared by every workspace. */
export const useRailOrderStore = create<RailOrderState>()(
  persist(
    (set) => ({
      order: null,
      setOrder: (next) => {
        const order = resolveRailOrder(next);
        const isDefault = order.every((id, i) => id === DEFAULT_RAIL_ORDER[i]);
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
