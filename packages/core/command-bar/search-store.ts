"use client";

import { create } from "zustand";

interface SearchStore {
  open: boolean;
  /**
   * The page to open on, for a shortcut that goes straight to one of the
   * palette's pages. Opaque here: the palette owns its page types.
   */
  launchPage: unknown;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  openOnPage: (page: unknown) => void;
  clearLaunchPage: () => void;
}

export const useSearchStore = create<SearchStore>((set) => ({
  open: false,
  launchPage: null,
  setOpen: (open) => set({ open }),
  toggle: () => set((s) => ({ open: !s.open })),
  openOnPage: (page) => set({ open: true, launchPage: page }),
  clearLaunchPage: () => set({ launchPage: null }),
}));
