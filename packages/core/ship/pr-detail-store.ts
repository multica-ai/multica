// PR detail drawer state — Zustand store keeping which PR is open in
// the drawer (if any). Lives in core/ rather than views/ per CLAUDE.md
// "Stores live in packages/core/ even when view-related, because
// stores are pure state, not UI."
//
// Why a store rather than local component state:
//   - The drawer is openable from the Kanban AND from the release
//     detail page's PR list. Both surfaces share the same drawer.
//   - The card click handler doesn't know how the page laid out the
//     drawer; it just dispatches `open(prId)`.
//   - WS events that arrive while the drawer is open need to refresh
//     the bundled query without prop-drilling the prId through the
//     entire surface.
//
// The state is purely client-side and ephemeral — never persisted.
// A workspace switch / page reload should wipe the open-drawer state.

import { create } from "zustand";

interface ShipPrDetailState {
  /** id of the PR currently displayed in the drawer, or null. */
  openPrId: string | null;
  /** Open the drawer with the given PR id. Idempotent — calling
   *  with the same id is a no-op (avoids a render thrash when the
   *  card's click handler fires twice). */
  open: (prId: string) => void;
  /** Close the drawer. */
  close: () => void;
}

export const useShipPrDetailStore = create<ShipPrDetailState>()((set, get) => ({
  openPrId: null,
  open: (prId) => {
    if (!prId) return;
    if (get().openPrId === prId) return;
    set({ openPrId: prId });
  },
  close: () => {
    if (get().openPrId === null) return;
    set({ openPrId: null });
  },
}));

/** Convenience selector — returns the currently open PR id (or null).
 *  Returns a primitive so the selector stays stable across renders. */
export function useShipPrDetailOpenId(): string | null {
  return useShipPrDetailStore((s) => s.openPrId);
}
