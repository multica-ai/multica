"use client";

import { useSearchStore } from "@multica/core/command-bar";
import type { PaletteStackEntry } from "./types";

/** Open ⌘K straight on `entry`'s page, as if its command had been chosen. ⌫ goes back to the root. */
export function openPalettePage(entry: PaletteStackEntry) {
  useSearchStore.getState().openOnPage(entry);
}

export function usePendingPalettePage(): PaletteStackEntry | null {
  return useSearchStore((s) => s.launchPage as PaletteStackEntry | null);
}

export function clearPendingPalettePage() {
  useSearchStore.getState().clearLaunchPage();
}
