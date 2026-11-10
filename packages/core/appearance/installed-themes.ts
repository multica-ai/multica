import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import { parseStoredTheme, type ThemePackageResult } from "./theme-package";
import { findBuiltInTheme } from "./built-in-themes";
import type { Theme } from "./themes";

/**
 * Themes installed from a folder or .zip, saved on this device. Stored packages are
 * parsed again on load, so a theme that a later check would reject never
 * gets applied.
 *
 * On Desktop, themes can also come from the themes folder. Those mirror the
 * folder: `syncFolder` replaces them whenever it changes, and `folders`
 * remembers which folder each one was read from, so a folder that stops
 * passing the checks keeps its last good version.
 */

export const INSTALLED_THEMES_STORAGE_KEY = "multica_installed_themes";
export const MAX_INSTALLED_THEMES = 20;

/** One entry of the themes folder, read and checked. */
export interface ThemeFolderEntry {
  /** The folder or .zip name inside the themes folder. */
  folder: string;
  result: ThemePackageResult;
}

interface InstalledThemesState {
  themes: Theme[];
  /** Theme id to the themes-folder entry it came from. */
  folders: Readonly<Record<string, string>>;
  /** Adds the theme, or replaces the installed one with the same id. */
  install: (theme: Theme) => void;
  uninstall: (id: string) => void;
  /** Makes the folder themes match the themes folder's current contents. */
  syncFolder: (entries: readonly ThemeFolderEntry[]) => void;
}

const isRecord = (v: unknown): v is Record<string, unknown> =>
  typeof v === "object" && v !== null && !Array.isArray(v);

function revalidate(stored: unknown): Pick<InstalledThemesState, "themes" | "folders"> {
  const record = isRecord(stored) ? stored : {};
  const list = Array.isArray(record.themes) ? record.themes : [];
  const themes: Theme[] = [];
  for (const raw of list.slice(0, MAX_INSTALLED_THEMES)) {
    const result = parseStoredTheme(raw);
    if (result.ok && !themes.some((t) => t.id === result.theme.id)) themes.push(result.theme);
  }
  const folders: Record<string, string> = {};
  if (isRecord(record.folders)) {
    for (const [id, folder] of Object.entries(record.folders)) {
      if (typeof folder === "string" && themes.some((t) => t.id === id)) folders[id] = folder;
    }
  }
  return { themes, folders };
}

export const useInstalledThemesStore = create<InstalledThemesState>()(
  persist(
    (set) => ({
      themes: [],
      folders: {},
      install: (theme) =>
        set((state) => {
          const others = state.themes.filter((t) => t.id !== theme.id);
          const { [theme.id]: _replaced, ...folders } = state.folders;
          return { themes: [...others, theme].slice(-MAX_INSTALLED_THEMES), folders };
        }),
      uninstall: (id) =>
        set((state) => {
          const { [id]: _removed, ...folders } = state.folders;
          return { themes: state.themes.filter((t) => t.id !== id), folders };
        }),
      syncFolder: (entries) =>
        set((state) => {
          const fromFolder = new Map<string, Theme>();
          const folders: Record<string, string> = {};
          for (const { folder, result } of entries) {
            if (result.ok) {
              if (fromFolder.has(result.theme.id) || findBuiltInTheme(result.theme.id)) continue;
              fromFolder.set(result.theme.id, result.theme);
              folders[result.theme.id] = folder;
              continue;
            }
            const lastGood = Object.entries(state.folders).find(([, f]) => f === folder)?.[0];
            const theme = lastGood ? state.themes.find((t) => t.id === lastGood) : undefined;
            if (theme && !fromFolder.has(theme.id)) {
              fromFolder.set(theme.id, theme);
              folders[theme.id] = folder;
            }
          }
          const imported = state.themes.filter((t) => !(t.id in state.folders) && !fromFolder.has(t.id));
          const themes = [...imported, ...fromFolder.values()].slice(-MAX_INSTALLED_THEMES);
          const kept = new Set(themes.map((t) => t.id));
          return {
            themes,
            folders: Object.fromEntries(Object.entries(folders).filter(([id]) => kept.has(id))),
          };
        }),
    }),
    {
      name: INSTALLED_THEMES_STORAGE_KEY,
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ themes: state.themes, folders: state.folders }),
      merge: (persisted, current) => ({ ...current, ...revalidate(persisted) }),
    },
  ),
);

interface ThemeFolderStatus {
  /** Absolute path of Desktop's themes folder; null until read, and on web. */
  path: string | null;
  /** Entries that fail the checks, with what is wrong. */
  problems: readonly { folder: string; errors: readonly string[] }[];
}

/** What the last read of the themes folder found, for the Appearance page. */
export const useThemeFolderStatus = create<ThemeFolderStatus>()(() => ({ path: null, problems: [] }));

/** A built-in or installed theme by id. */
export function findTheme(id: string): Theme | undefined {
  return (
    findBuiltInTheme(id) ?? useInstalledThemesStore.getState().themes.find((theme) => theme.id === id)
  );
}
