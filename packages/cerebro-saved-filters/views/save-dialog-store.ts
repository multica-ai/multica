import { create } from "zustand";

import type { FilterSnapshot } from "../core/types";

/**
 * The "Save current filter…" action lives inside the Filter dropdown, which
 * unmounts its content the moment an item is selected. The naming + sharing
 * dialog must therefore live OUTSIDE the dropdown, in a persistent host. This
 * tiny store is the bridge between the dropdown items and that host.
 *
 * Two modes:
 *   "create" — name + share a brand-new filter from the current snapshot.
 *   "edit"   — re-name / re-share an existing owned filter (the host loads its
 *              current state by id).
 */
interface SaveDialogState {
  open: boolean;
  mode: "create" | "edit";
  /** Snapshot to persist in create mode. */
  snapshot: FilterSnapshot | null;
  /** Existing filter id in edit mode. */
  filterId: string | null;
  requestSave: (snapshot: FilterSnapshot) => void;
  requestEdit: (filterId: string) => void;
  close: () => void;
}

export const useSaveFilterDialogStore = create<SaveDialogState>((set) => ({
  open: false,
  mode: "create",
  snapshot: null,
  filterId: null,
  requestSave: (snapshot) =>
    set({ open: true, mode: "create", snapshot, filterId: null }),
  requestEdit: (filterId) =>
    set({ open: true, mode: "edit", snapshot: null, filterId }),
  close: () => set({ open: false, snapshot: null, filterId: null }),
}));
