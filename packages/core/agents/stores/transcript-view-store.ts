"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type TranscriptSortDirection = "chronological" | "newest_first";
export type TranscriptFilterKey = string;

interface TranscriptViewState {
  sortDirection: TranscriptSortDirection;
  selectedFilterKeys: TranscriptFilterKey[];
  setSortDirection: (dir: TranscriptSortDirection) => void;
  setSelectedFilterKeys: (keys: TranscriptFilterKey[]) => void;
  toggleFilterKey: (key: TranscriptFilterKey) => void;
  clearFilterKeys: () => void;
}

const DEFAULTS = {
  sortDirection: "chronological" as TranscriptSortDirection,
  selectedFilterKeys: [] as TranscriptFilterKey[],
};

function uniqueFilterKeys(keys: TranscriptFilterKey[]): TranscriptFilterKey[] {
  return Array.from(new Set(keys.filter((key) => key.length > 0)));
}

export const useTranscriptViewStore = create<TranscriptViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      setSortDirection: (sortDirection) => set({ sortDirection }),
      setSelectedFilterKeys: (selectedFilterKeys) =>
        set({ selectedFilterKeys: uniqueFilterKeys(selectedFilterKeys) }),
      toggleFilterKey: (key) =>
        set((state) => ({
          selectedFilterKeys: state.selectedFilterKeys.includes(key)
            ? state.selectedFilterKeys.filter((candidate) => candidate !== key)
            : [...state.selectedFilterKeys, key],
        })),
      clearFilterKeys: () => set({ selectedFilterKeys: [] }),
    }),
    {
      name: "multica_transcript_view",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({
        sortDirection: state.sortDirection,
        selectedFilterKeys: state.selectedFilterKeys,
      }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<TranscriptViewState>;
        return {
          ...current,
          sortDirection: p.sortDirection ?? DEFAULTS.sortDirection,
          selectedFilterKeys: uniqueFilterKeys(p.selectedFilterKeys ?? []),
        };
      },
    },
  ),
);
