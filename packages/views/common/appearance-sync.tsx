"use client";

import { useEffect, useLayoutEffect } from "react";
import {
  APPEARANCE_STORAGE_KEY,
  APPEARANCE_STYLE_ATTR,
  appearanceStyleAttribute,
  useAppearanceStore,
} from "@multica/core/appearance";

/**
 * Mirrors the appearance preference onto `<html>`, where appearance.css keys
 * on it. Mount it only around the product UI: unmounting clears the
 * attribute, so pages outside it (web's landing and auth pages) keep the
 * default look. A change made in another window or tab follows via the
 * storage event.
 */
export function AppearanceSync() {
  const style = useAppearanceStore((s) => s.style);

  useLayoutEffect(() => {
    const root = document.documentElement;
    const value = appearanceStyleAttribute(style);
    if (value === null) root.removeAttribute(APPEARANCE_STYLE_ATTR);
    else root.setAttribute(APPEARANCE_STYLE_ATTR, value);
    return () => root.removeAttribute(APPEARANCE_STYLE_ATTR);
  }, [style]);

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === APPEARANCE_STORAGE_KEY) void useAppearanceStore.persist.rehydrate();
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  return null;
}
