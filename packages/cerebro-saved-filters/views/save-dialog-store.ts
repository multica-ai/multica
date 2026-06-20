import { create } from "zustand";

import type { FilterSnapshot } from "../core/types";

/**
 * The "Save current filter…" action lives inside the Filter dropdown, which
 * unmounts its content the moment an item is selected. The naming dialog must
 * therefore live OUTSIDE the dropdown, in a persistent host. This tiny store is
 * the bridge: the dropdown item captures the current snapshot and opens the
 * dialog; the host (mounted next to the dropdown) renders it.
 */
interface SaveDialogState {
  open: boolean;
  snapshot: FilterSnapshot | null;
  requestSave: (snapshot: FilterSnapshot) => void;
  close: () => void;
}

export const useSaveFilterDialogStore = create<SaveDialogState>((set) => ({
  open: false,
  snapshot: null,
  requestSave: (snapshot) => set({ open: true, snapshot }),
  close: () => set({ open: false, snapshot: null }),
}));
