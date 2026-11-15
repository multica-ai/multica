"use client";

import { createContext, useContext } from "react";
import type { PaletteHostDialog, PaletteStackEntry } from "./types";

export interface PaletteApi {
  close: () => void;
  push: (entry: PaletteStackEntry) => void;
  /** Navigate honoring the gesture's modifiers (⌘↵ / ⌘-click open a new tab), then close. */
  navigate: (href: string) => void;
  /** Close the palette and open a confirm or form dialog it hosts. */
  openHostDialog: (dialog: PaletteHostDialog) => void;
}

export const PaletteContext = createContext<PaletteApi | null>(null);

export function usePalette(): PaletteApi {
  const api = useContext(PaletteContext);
  if (!api) throw new Error("usePalette must be used inside SearchCommand");
  return api;
}
