"use client";

import { useEffect, useLayoutEffect } from "react";
import {
  APPEARANCE_FRAME_ATTR,
  APPEARANCE_STORAGE_KEY,
  APPEARANCE_STYLE_ATTR,
  appearanceAttributes,
  useAppearanceStore,
} from "@multica/core/appearance";

/**
 * Mirrors the appearance preference onto `<html>`, where appearance.css keys
 * on it. Mount it only around the product UI: unmounting clears the
 * attributes, so pages outside it (web's landing and auth pages) keep the
 * default look. A change made in another window or tab follows via the
 * storage event.
 */
export function AppearanceSync() {
  const style = useAppearanceStore((s) => s.style);
  const frame = useAppearanceStore((s) => s.frame);

  useLayoutEffect(() => {
    const root = document.documentElement;
    for (const [name, value] of Object.entries(appearanceAttributes({ style, frame }))) {
      if (value === null) root.removeAttribute(name);
      else root.setAttribute(name, value);
    }
    return () => {
      root.removeAttribute(APPEARANCE_STYLE_ATTR);
      root.removeAttribute(APPEARANCE_FRAME_ATTR);
    };
  }, [style, frame]);

  useEffect(() => {
    const onStorage = (event: StorageEvent) => {
      if (event.key === APPEARANCE_STORAGE_KEY) void useAppearanceStore.persist.rehydrate();
    };
    window.addEventListener("storage", onStorage);
    return () => window.removeEventListener("storage", onStorage);
  }, []);

  return null;
}
