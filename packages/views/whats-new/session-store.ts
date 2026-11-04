"use client";

import { create } from "zustand";

/**
 * Page-session state shared by every mounted launcher. Narrow layouts mount
 * a hidden rail and the compact nav sheet's rail side by side, and the sheet
 * remounts on every open, so a per-component flag would re-show a card the
 * user already clicked away from.
 */
interface WhatsNewSessionStore {
  autoShown: boolean;
  markAutoShown: () => void;
}

export const useWhatsNewSessionStore = create<WhatsNewSessionStore>((set) => ({
  autoShown: false,
  markAutoShown: () => set({ autoShown: true }),
}));
