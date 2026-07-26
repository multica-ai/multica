"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../../platform/storage";

export type TranscriptSortDirection = "chronological" | "newest_first";
export type TranscriptFilterKey = string;

/**
 * Persisted expand-mode preference for the transcript. `smart` applies the
 * per-kind reading hierarchy (agent text and errors open, process noise
 * folded); `expanded`/`collapsed` are wholesale overrides. Row-level manual
 * toggles live in the dialog, sit above all three, and reset on mode switch.
 */
export type TranscriptDetailDensity = "smart" | "expanded" | "collapsed";

const DENSITIES: readonly TranscriptDetailDensity[] = ["smart", "expanded", "collapsed"];

interface TranscriptViewState {
  sortDirection: TranscriptSortDirection;
  selectedFilterKeys: TranscriptFilterKey[];
  density: TranscriptDetailDensity;
  setSortDirection: (dir: TranscriptSortDirection) => void;
  setSelectedFilterKeys: (keys: TranscriptFilterKey[]) => void;
  toggleFilterKey: (key: TranscriptFilterKey) => void;
  clearFilterKeys: () => void;
  setDensity: (density: TranscriptDetailDensity) => void;
}

const DEFAULTS = {
  sortDirection: "chronological" as TranscriptSortDirection,
  selectedFilterKeys: [] as TranscriptFilterKey[],
  density: "smart" as TranscriptDetailDensity,
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
      setDensity: (density) => set({ density }),
    }),
    {
      name: "multica_transcript_view",
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({
        sortDirection: state.sortDirection,
        selectedFilterKeys: state.selectedFilterKeys,
        density: state.density,
      }),
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<TranscriptViewState> & {
          /** Pre-density persisted shape: a boolean expand-everything flag. */
          defaultExpanded?: boolean;
        };
        const density = DENSITIES.includes(p.density as TranscriptDetailDensity)
          ? (p.density as TranscriptDetailDensity)
          : p.defaultExpanded === true
            ? "expanded"
            : DEFAULTS.density;
        return {
          ...current,
          sortDirection: p.sortDirection ?? DEFAULTS.sortDirection,
          selectedFilterKeys: uniqueFilterKeys(p.selectedFilterKeys ?? []),
          density,
        };
      },
    },
  ),
);
