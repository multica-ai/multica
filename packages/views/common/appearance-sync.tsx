"use client";

import { useEffect, useLayoutEffect } from "react";
import { useShallow } from "zustand/react/shallow";
import {
  APPEARANCE_STORAGE_KEY,
  APPEARANCE_THEME_ATTR,
  INSTALLED_THEMES_STORAGE_KEY,
  THEME_OPTION_ATTR_PREFIX,
  activeTheme,
  appearanceRootVariables,
  appearanceStylesheet,
  appearanceThemeAttributes,
  appearanceThemeCss,
  selectAppearance,
  themeFromFolderFiles,
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeFolderStatus,
  useThemeSafeMode,
  type ThemeFolderEntry,
} from "@multica/core/appearance";
import { getShortcutPlatform, getShortcutRuntime } from "@multica/core/shortcuts";
import { themeFolderAPI } from "../platform/theme-folder";

const STYLE_ELEMENT_ID = "multica-appearance";
const THEME_CSS_ELEMENT_ID = "multica-theme-css";
/** Safe mode: Shift pressed within this long after load and held for SAFE_MODE_HOLD_MS. */
const SAFE_MODE_WINDOW_MS = 3000;
const SAFE_MODE_HOLD_MS = 700;

/** Creates, updates or removes a `<style>` at the end of <head>. */
function writeStyle(id: string, css: string) {
  let style = document.getElementById(id);
  if (!css) {
    style?.remove();
    return;
  }
  if (!style) {
    style = document.createElement("style");
    style.id = id;
    document.head.appendChild(style);
  }
  if (style.textContent !== css) style.textContent = css;
}

/** The adjustments go after the theme's CSS, whichever was created first. */
function orderStyles() {
  const theme = document.getElementById(THEME_CSS_ELEMENT_ID);
  const adjustments = document.getElementById(STYLE_ELEMENT_ID);
  if (theme && adjustments && adjustments.compareDocumentPosition(theme) & Node.DOCUMENT_POSITION_FOLLOWING) {
    document.head.appendChild(adjustments);
  }
}

/** `theme-light` / `theme-dark` on <html>, following the `dark` class the mode switch sets. */
function useModeClasses() {
  useLayoutEffect(() => {
    const root = document.documentElement;
    const sync = () => {
      const dark = root.classList.contains("dark");
      if (root.classList.contains("theme-dark") !== dark) root.classList.toggle("theme-dark", dark);
      if (root.classList.contains("theme-light") === dark) root.classList.toggle("theme-light", !dark);
    };
    sync();
    const observer = new MutationObserver(sync);
    observer.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => {
      observer.disconnect();
      root.classList.remove("theme-light", "theme-dark");
    };
  }, []);
}

/** `is-desktop` / `is-web`, `mod-<os>` and `is-focused` on <html>, for theme CSS. */
function useStateClasses() {
  useLayoutEffect(() => {
    const root = document.documentElement;
    const platform = getShortcutPlatform();
    const fixed = [
      getShortcutRuntime() === "desktop" ? "is-desktop" : "is-web",
      ...(platform === "unknown" ? [] : [`mod-${platform}`]),
    ];
    root.classList.add(...fixed);
    const focus = () => root.classList.toggle("is-focused", document.hasFocus());
    focus();
    window.addEventListener("focus", focus);
    window.addEventListener("blur", focus);
    return () => {
      window.removeEventListener("focus", focus);
      window.removeEventListener("blur", focus);
      root.classList.remove(...fixed, "is-focused");
    };
  }, []);
}

/**
 * On Desktop, installs the themes in the themes folder and reinstalls them
 * whenever main reports a change, so a theme being edited updates as it is
 * saved. Reads never overlap; a change during a read triggers one more.
 */
function useThemeFolderSync() {
  useEffect(() => {
    const api = themeFolderAPI();
    if (!api) return;
    let stopped = false;
    let reading = false;
    let again = false;
    const read = async () => {
      if (reading) {
        again = true;
        return;
      }
      reading = true;
      try {
        const snapshot = await api.readThemeFolder();
        const entries: ThemeFolderEntry[] = await Promise.all(
          snapshot.entries.map(async ({ folder, files, error }) => ({
            folder,
            result: files ? await themeFromFolderFiles(files) : { ok: false as const, errors: [error ?? ""] },
          })),
        );
        if (stopped) return;
        useInstalledThemesStore.getState().syncFolder(entries);
        useThemeFolderStatus.setState({
          path: snapshot.path,
          problems: entries.flatMap(({ folder, result }) => (result.ok ? [] : [{ folder, errors: result.errors }])),
        });
      } catch {
        // The folder could not be read; installed themes stay as they are.
      } finally {
        reading = false;
        if (again && !stopped) {
          again = false;
          void read();
        }
      }
    };
    void read();
    const unsubscribe = api.onThemeFolderChange(() => void read());
    return () => {
      stopped = true;
      unsubscribe();
    };
  }, []);
}

function clearThemeAttributes(root: HTMLElement) {
  root.removeAttribute(APPEARANCE_THEME_ATTR);
  for (const name of root.getAttributeNames()) {
    if (name.startsWith(THEME_OPTION_ATTR_PREFIX)) root.removeAttribute(name);
  }
}

/**
 * Mirrors the appearance preferences onto `<html>`: the mode as
 * `theme-light` / `theme-dark`, the theme and its options as attributes, the
 * theme's CSS and the accent and contrast adjustments as two generated
 * stylesheets after the app's own, fonts, size and density as inline
 * variables. Mount it only around the product UI:
 * unmounting clears all of it, so pages outside it (web's landing and auth
 * pages) keep the default look. Changes made in another window or tab follow
 * via the storage event.
 */
export function AppearanceSync() {
  const prefs = useAppearanceStore(useShallow(selectAppearance));
  const installed = useInstalledThemesStore((s) => s.themes);
  const safeMode = useThemeSafeMode((s) => s.active);
  useModeClasses();
  useStateClasses();
  useThemeFolderSync();

  useLayoutEffect(() => {
    const root = document.documentElement;
    const theme = activeTheme(prefs, safeMode);

    clearThemeAttributes(root);
    for (const [name, value] of Object.entries(appearanceThemeAttributes(theme, prefs))) {
      if (value !== null) root.setAttribute(name, value);
    }

    const variables = appearanceRootVariables(prefs);
    for (const [name, value] of Object.entries(variables)) {
      if (value === null) root.style.removeProperty(name);
      else root.style.setProperty(name, value);
    }

    writeStyle(THEME_CSS_ELEMENT_ID, appearanceThemeCss(theme));
    writeStyle(STYLE_ELEMENT_ID, appearanceStylesheet(theme, prefs));
    orderStyles();

    return () => {
      clearThemeAttributes(root);
      for (const name of Object.keys(variables)) root.style.removeProperty(name);
      document.getElementById(STYLE_ELEMENT_ID)?.remove();
      document.getElementById(THEME_CSS_ELEMENT_ID)?.remove();
    };
  }, [prefs, installed, safeMode]);

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === APPEARANCE_STORAGE_KEY) void useAppearanceStore.persist.rehydrate();
      if (event.key === INSTALLED_THEMES_STORAGE_KEY) void useInstalledThemesStore.persist.rehydrate();
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  useEffect(() => {
    // Modifier keys do not auto-repeat on macOS, so a hold is timed instead.
    let hold: number | undefined;
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key !== "Shift" || hold !== undefined) return;
      hold = window.setTimeout(() => useThemeSafeMode.getState().enter(), SAFE_MODE_HOLD_MS);
    };
    const onKeyUp = (event: KeyboardEvent) => {
      if (event.key !== "Shift") return;
      window.clearTimeout(hold);
      hold = undefined;
    };
    window.addEventListener("keydown", onKeyDown);
    window.addEventListener("keyup", onKeyUp);
    const stopListening = window.setTimeout(
      () => window.removeEventListener("keydown", onKeyDown),
      SAFE_MODE_WINDOW_MS,
    );
    return () => {
      window.clearTimeout(stopListening);
      window.clearTimeout(hold);
      window.removeEventListener("keydown", onKeyDown);
      window.removeEventListener("keyup", onKeyUp);
    };
  }, []);

  return null;
}
