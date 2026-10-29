"use client";

import { create } from "zustand";

interface WorkspaceSwitcherStore {
  isOpen: boolean;
  requestedIndex: number | null;
  setOpen: (open: boolean) => void;
  toggle: () => void;
  requestSelection: (index: number) => boolean;
  clearSelection: () => void;
}

// Ephemeral window UI state survives workspace route changes. The renderer
// can route Electron's numbered shortcuts here without duplicating its IPC.
export const useWorkspaceSwitcherStore = create<WorkspaceSwitcherStore>((set, get) => ({
  isOpen: false,
  requestedIndex: null,
  setOpen: (isOpen) => set({ isOpen, requestedIndex: null }),
  toggle: () => set((state) => ({ isOpen: !state.isOpen, requestedIndex: null })),
  requestSelection: (requestedIndex) => {
    if (!get().isOpen) return false;
    set({ requestedIndex });
    return true;
  },
  clearSelection: () => set({ requestedIndex: null }),
}));
