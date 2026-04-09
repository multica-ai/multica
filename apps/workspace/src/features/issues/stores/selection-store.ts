"use client";

import { create } from "zustand";

interface IssueSelectionState {
  selectedIds: Set<string>;
  toggle: (id: string) => void;
  select: (ids: string[]) => void;
  deselect: (ids: string[]) => void;
  clear: () => void;
}

export const useIssueSelectionStore = create<IssueSelectionState>()((set) => ({
  selectedIds: new Set<string>(),
  toggle: (id) =>
    set((state) => {
      const next = new Set(state.selectedIds);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return { selectedIds: next };
    }),
  select: (ids) =>
    set((state) => {
      const next = new Set(state.selectedIds);
      for (const id of ids) next.add(id);
      return { selectedIds: next };
    }),
  deselect: (ids) =>
    set((state) => {
      const next = new Set(state.selectedIds);
      for (const id of ids) next.delete(id);
      return { selectedIds: next };
    }),
  clear: () => set({ selectedIds: new Set() }),
}));
