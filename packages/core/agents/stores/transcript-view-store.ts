"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type TranscriptSortDirection = "chronological" | "newest_first";

interface TranscriptViewState {
  sortDirection: TranscriptSortDirection;
  setSortDirection: (dir: TranscriptSortDirection) => void;
}

export const useTranscriptViewStore = create<TranscriptViewState>()(
  persist(
    (set) => ({
      sortDirection: "chronological",
      setSortDirection: (sortDirection) => set({ sortDirection }),
    }),
    {
      name: "multica_transcript_view",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ sortDirection: state.sortDirection }),
    },
  ),
);
