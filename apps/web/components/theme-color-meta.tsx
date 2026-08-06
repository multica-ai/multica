"use client";

import { useEffect } from "react";
import { applyThemeColorMeta, readPaintedThemeColor } from "@/lib/theme-color";

/**
 * Keeps `<meta name="theme-color">` on the colour the app is actually painting.
 *
 * That meta tints the Android status bar in the installed PWA and the address
 * bar in mobile Chrome. The server can only ship a `prefers-color-scheme`
 * guess, which follows the OS — so anyone running the app dark on a light phone
 * kept a light strip above a dark page. Re-reading on every theme change is
 * what lets the in-app toggle retint without a reload.
 */
export function ThemeColorMeta() {
  useEffect(() => {
    const sync = () => {
      const color = readPaintedThemeColor();
      if (color) applyThemeColorMeta(color);
    };

    sync();

    // The resolved theme is the class on <html>, and next-themes writes it from
    // the ThemeProvider's own effect — which React runs *after* the effects of
    // its descendants. Watching the mutation rather than depending on
    // `resolvedTheme` is what stops this reading the outgoing theme's colour,
    // and it also catches an OS-level flip while the app is set to "system",
    // which re-applies the class without re-rendering anything here.
    const observer = new MutationObserver(sync);
    observer.observe(document.documentElement, {
      attributes: true,
      attributeFilter: ["class"],
    });
    return () => observer.disconnect();
  }, []);

  return null;
}
