"use client";

import { useEffect } from "react";
import { useMobileAppearanceStore } from "./mobile-appearance-store";

export const FONT_SIZE_ATTRIBUTE = "data-mobile-font-size";
export const CONTENT_WIDTH_ATTRIBUTE = "data-mobile-content-width";

/**
 * Projects the stored preferences onto the root element. The stylesheet owns
 * what each value means (tokens.css), so this is the only runtime bridge.
 */
export function MobileAppearanceSync() {
  const fontSize = useMobileAppearanceStore((s) => s.fontSize);
  const contentWidth = useMobileAppearanceStore((s) => s.contentWidth);

  useEffect(() => {
    document.documentElement.setAttribute(FONT_SIZE_ATTRIBUTE, fontSize);
  }, [fontSize]);

  useEffect(() => {
    document.documentElement.setAttribute(CONTENT_WIDTH_ATTRIBUTE, contentWidth);
  }, [contentWidth]);

  return null;
}
