// Phase 7a — Multi-select selection store for the Ship Hub Kanban.
//
// Lives in core/ rather than views/ per CLAUDE.md "Stores live in
// packages/core/ even when view-related, because stores are pure
// state, not UI." The selection is purely client-side, ephemeral,
// and never persisted (per CLAUDE.md "Don't persist ephemeral UI
// state") — a workspace switch should clear it; a page reload
// should clear it.
//
// We use a Set<string> internally because the Kanban needs O(1)
// "is this PR selected?" lookups in the card render path. The
// store exposes both an array view (for components that want to
// iterate / pass the list to a mutation) and a `has` predicate.

import { create } from "zustand";

interface ShipSelectionState {
  /** Internal storage. Sets give O(1) toggles + lookups. */
  selected: Set<string>;

  /** Toggle a PR's selected state. */
  toggle: (prId: string) => void;
  /** Add a PR if not already in the set. Idempotent. */
  add: (prId: string) => void;
  /** Remove a PR from the set. Idempotent. */
  remove: (prId: string) => void;
  /** Replace the selection with the given list. Used by the
   *  multi-select dialog's "select all" button. */
  selectAll: (prIds: string[]) => void;
  /** Clear the selection — fired after release creation, on
   *  workspace switch, or when the user clicks the "Cancel" chip
   *  on the selection bar. */
  clear: () => void;
  /** Predicate for the card render. Reads use the underlying Set
   *  for stable references. */
  has: (prId: string) => boolean;
}

// We expose the store hook directly. Per CLAUDE.md "Selectors must
// return stable references" — components select primitives or use
// the dedicated count hook below rather than building a fresh
// array on every render.
export const useShipSelection = create<ShipSelectionState>()((set, get) => ({
  selected: new Set<string>(),
  toggle: (prId) => {
    set((state) => {
      const next = new Set(state.selected);
      if (next.has(prId)) {
        next.delete(prId);
      } else {
        next.add(prId);
      }
      return { selected: next };
    });
  },
  add: (prId) => {
    set((state) => {
      if (state.selected.has(prId)) return state;
      const next = new Set(state.selected);
      next.add(prId);
      return { selected: next };
    });
  },
  remove: (prId) => {
    set((state) => {
      if (!state.selected.has(prId)) return state;
      const next = new Set(state.selected);
      next.delete(prId);
      return { selected: next };
    });
  },
  selectAll: (prIds) => {
    set({ selected: new Set(prIds) });
  },
  clear: () => {
    if (get().selected.size === 0) return;
    set({ selected: new Set() });
  },
  has: (prId) => get().selected.has(prId),
}));

/** Selects just the count. Returning the size primitive (number)
 *  avoids the "fresh-array-each-render" footgun documented in
 *  CLAUDE.md (selectors must return stable references). */
export function useShipSelectionCount(): number {
  return useShipSelection((s) => s.selected.size);
}
