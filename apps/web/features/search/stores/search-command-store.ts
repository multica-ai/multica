import { create } from "zustand";

interface SearchCommandState {
  open: boolean;
  toggle: () => void;
  setOpen: (open: boolean) => void;
}

export const useSearchCommandStore = create<SearchCommandState>((set) => ({
  open: false,
  toggle: () => set((s) => ({ open: !s.open })),
  setOpen: (open) => set({ open }),
}));
