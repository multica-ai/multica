"use client";

import { useEffect, useLayoutEffect } from "react";
import { useShallow } from "zustand/react/shallow";
import {
  APPEARANCE_STORAGE_KEY,
  APPEARANCE_THEME_ATTR,
  APPEARANCE_TRAITS_ATTR,
  appearanceRootVariables,
  appearanceStylesheet,
  appearanceThemeAttributes,
  selectAppearance,
  useAppearanceStore,
} from "@multica/core/appearance";

const STYLE_ELEMENT_ID = "multica-appearance";

function setAttribute(root: HTMLElement, name: string, value: string | null) {
  if (value === null) root.removeAttribute(name);
  else root.setAttribute(name, value);
}

/**
 * Mirrors the appearance preferences onto `<html>`: the theme as attributes
 * plus one generated stylesheet, fonts, size and density as inline
 * variables. Mount it only around the product UI: unmounting clears all of
 * it, so pages outside it (web's landing and auth pages) keep the default
 * look. A change made in another window or tab follows via the storage event.
 */
export function AppearanceSync() {
  const prefs = useAppearanceStore(useShallow(selectAppearance));

  useLayoutEffect(() => {
    const root = document.documentElement;
    const { theme, traits } = appearanceThemeAttributes(prefs);
    setAttribute(root, APPEARANCE_THEME_ATTR, theme);
    setAttribute(root, APPEARANCE_TRAITS_ATTR, traits);

    const variables = appearanceRootVariables(prefs);
    for (const [name, value] of Object.entries(variables)) {
      if (value === null) root.style.removeProperty(name);
      else root.style.setProperty(name, value);
    }

    const css = appearanceStylesheet(prefs);
    let style = document.getElementById(STYLE_ELEMENT_ID);
    if (css && !style) {
      style = document.createElement("style");
      style.id = STYLE_ELEMENT_ID;
      document.head.appendChild(style);
    }
    if (style) {
      if (css) style.textContent = css;
      else style.remove();
    }

    return () => {
      root.removeAttribute(APPEARANCE_THEME_ATTR);
      root.removeAttribute(APPEARANCE_TRAITS_ATTR);
      for (const name of Object.keys(variables)) root.style.removeProperty(name);
      document.getElementById(STYLE_ELEMENT_ID)?.remove();
    };
  }, [prefs]);

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === APPEARANCE_STORAGE_KEY) void useAppearanceStore.persist.rehydrate();
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  return null;
}
