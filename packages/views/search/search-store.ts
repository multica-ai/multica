"use client";

import { create } from "zustand";

interface SearchStore {
  open: boolean;
  setOpen: (open: boolean) => void;
  toggle: () => void;
}

export const useSearchStore = create<SearchStore>((set) => ({
  open: false,
  setOpen: (open) => set({ open }),
  toggle: () => set((s) => ({ open: !s.open })),
}));
