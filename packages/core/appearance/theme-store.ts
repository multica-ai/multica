import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { StoreTheme, StoreThemeMode } from "../api/schemas";
import { findBuiltInTheme } from "./built-in-themes";
import { readZip } from "./theme-archive";
import { THEME_PACKAGE_MAX_BYTES, themeFromFiles, type ThemePackageResult } from "./theme-package";
import type { Theme } from "./themes";

/**
 * The community theme store (beta). The registry lists themes; the server
 * serves each one's checked release, and it is read here with the same
 * parser and CSS sanitizer as a theme someone imports. A store install is
 * saved on this device like an import, with the release it came from.
 */

export type { StoreTheme, StoreThemeMode };

export const themeStoreKeys = {
  all: ["theme-store"] as const,
  themes: () => [...themeStoreKeys.all, "themes"] as const,
  screenshot: (id: string, version: string, mode: StoreThemeMode) =>
    [...themeStoreKeys.all, "screenshot", id, version, mode] as const,
};

export const storeThemesOptions = (enabled: boolean) =>
  queryOptions({
    queryKey: themeStoreKeys.themes(),
    queryFn: () => api.listStoreThemes(),
    enabled,
    staleTime: 5 * 60 * 1000,
  });

/** A version's screenshot never changes, so it is fetched once. */
export const storeThemeScreenshotOptions = (theme: StoreTheme, mode: StoreThemeMode) =>
  queryOptions({
    queryKey: themeStoreKeys.screenshot(theme.id, theme.version, mode),
    queryFn: ({ signal }) => api.getStoreThemeScreenshot(theme.id, mode, signal),
    enabled: theme.screenshots.includes(mode),
    staleTime: Infinity,
    gcTime: 30 * 60 * 1000,
  });

/**
 * Where a store theme stands on this device:
 * - `available`: not installed;
 * - `installed`: installed from the store at the listed version;
 * - `update`: installed from the store at another version;
 * - `taken`: its id belongs to a built-in, imported or themes-folder theme,
 *   which a store install must not replace.
 */
export type StoreThemeState = "available" | "installed" | "update" | "taken";

export function storeThemeState(
  theme: StoreTheme,
  installed: { themes: readonly Theme[]; fromStore: Readonly<Record<string, string>> },
): StoreThemeState {
  if (findBuiltInTheme(theme.id)) return "taken";
  const version = installed.fromStore[theme.id];
  if (version !== undefined && installed.themes.some((t) => t.id === theme.id)) {
    return version === theme.version ? "installed" : "update";
  }
  return installed.themes.some((t) => t.id === theme.id) ? "taken" : "available";
}

/**
 * Downloads the theme's release and reads it as an imported theme is read.
 * The package must be the theme and release the registry lists.
 */
export async function readStoreTheme(theme: StoreTheme): Promise<ThemePackageResult> {
  const bytes = await api.getStoreThemePackage(theme.id);
  if (bytes.length > THEME_PACKAGE_MAX_BYTES) {
    return { ok: false, errors: [`A theme can be at most ${THEME_PACKAGE_MAX_BYTES / 1024} KB`] };
  }
  const archive = await readZip(bytes, THEME_PACKAGE_MAX_BYTES);
  if (!archive.ok) return { ok: false, errors: [archive.error] };
  const result = themeFromFiles(archive.files);
  if (!result.ok) return result;
  if (result.theme.id !== theme.id) {
    return { ok: false, errors: [`The release is the theme "${result.theme.id}", not "${theme.id}"`] };
  }
  if (result.theme.version !== theme.version) {
    return {
      ok: false,
      errors: [`The release is version ${result.theme.version ?? "(none)"}, not ${theme.version}`],
    };
  }
  return result;
}
