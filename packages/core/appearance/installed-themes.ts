import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { parseThemePackage } from "./theme-package";
import { findBuiltInTheme, type ThemeManifest } from "./themes";

/**
 * Themes installed from a file, saved on this device. Stored packages are
 * parsed again on load, so a theme that a later check would reject never
 * gets applied.
 */

export const INSTALLED_THEMES_STORAGE_KEY = "multica_installed_themes";
export const MAX_INSTALLED_THEMES = 20;

interface InstalledThemesState {
  themes: ThemeManifest[];
  /** Adds the theme, or replaces the installed one with the same id. */
  install: (theme: ThemeManifest) => void;
  uninstall: (id: string) => void;
}

function revalidate(stored: unknown): ThemeManifest[] {
  const list =
    stored && typeof stored === "object" && Array.isArray((stored as { themes?: unknown }).themes)
      ? ((stored as { themes: unknown[] }).themes)
      : [];
  const themes: ThemeManifest[] = [];
  for (const raw of list.slice(0, MAX_INSTALLED_THEMES)) {
    const result = parseThemePackage(raw);
    if (result.ok && !themes.some((t) => t.id === result.theme.id)) themes.push(result.theme);
  }
  return themes;
}

export const useInstalledThemesStore = create<InstalledThemesState>()(
  persist(
    (set) => ({
      themes: [],
      install: (theme) =>
        set((state) => {
          const others = state.themes.filter((t) => t.id !== theme.id);
          return { themes: [...others, theme].slice(-MAX_INSTALLED_THEMES) };
        }),
      uninstall: (id) => set((state) => ({ themes: state.themes.filter((t) => t.id !== id) })),
    }),
    {
      name: INSTALLED_THEMES_STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ themes: state.themes }),
      merge: (persisted, current) => ({ ...current, themes: revalidate(persisted) }),
    },
  ),
);

/** A built-in or installed theme by id. */
export function findTheme(id: string): ThemeManifest | undefined {
  return (
    findBuiltInTheme(id) ?? useInstalledThemesStore.getState().themes.find((theme) => theme.id === id)
  );
}
