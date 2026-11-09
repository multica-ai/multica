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
  useAppearanceStore,
  useInstalledThemesStore,
  useThemeSafeMode,
} from "@multica/core/appearance";

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

function clearThemeAttributes(root: HTMLElement) {
  root.removeAttribute(APPEARANCE_THEME_ATTR);
  for (const name of root.getAttributeNames()) {
    if (name.startsWith(THEME_OPTION_ATTR_PREFIX)) root.removeAttribute(name);
  }
}

/**
 * Mirrors the appearance preferences onto `<html>`: the theme and its
 * options as attributes, its tokens and its own CSS as two generated
 * stylesheets (the CSS one last, so it follows the app's styles), fonts, size
 * and density as inline variables. Mount it only around the product UI:
 * unmounting clears all of it, so pages outside it (web's landing and auth
 * pages) keep the default look. Changes made in another window or tab follow
 * via the storage event.
 */
export function AppearanceSync() {
  const prefs = useAppearanceStore(useShallow(selectAppearance));
  const installed = useInstalledThemesStore((s) => s.themes);
  const safeMode = useThemeSafeMode((s) => s.active);

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

    writeStyle(STYLE_ELEMENT_ID, appearanceStylesheet(theme, prefs));
    writeStyle(THEME_CSS_ELEMENT_ID, appearanceThemeCss(theme));

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
