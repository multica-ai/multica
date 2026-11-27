"use client";

import { useSyncExternalStore } from "react";

const STANDALONE_QUERY = "(display-mode: standalone)";

/**
 * True when the web app runs as an installed PWA (a home-screen icon launched
 * without browser chrome). There is no address bar, back button or close button
 * in that window, so anything that navigates the window away from the app to a
 * file strands the reader.
 *
 * iOS Safari reports its standalone mode through the non-standard
 * `navigator.standalone` as well as the `display-mode` media query; either one
 * counts. The Electron shell is never standalone in this sense — it has its own
 * window frame and routes files through the main process.
 *
 * Read at call time so the answer is never stale and SSR-safe: false without a
 * `window`.
 */
export function isStandalonePwa(): boolean {
  if (typeof window === "undefined") return false;
  if ((window as unknown as { desktopAPI?: unknown }).desktopAPI) return false;
  if ((navigator as Navigator & { standalone?: boolean }).standalone === true) {
    return true;
  }
  return window.matchMedia?.(STANDALONE_QUERY).matches === true;
}

function subscribe(onChange: () => void): () => void {
  if (typeof window === "undefined" || !window.matchMedia) return () => {};
  const query = window.matchMedia(STANDALONE_QUERY);
  query.addEventListener("change", onChange);
  return () => query.removeEventListener("change", onChange);
}

/**
 * Render-time `isStandalonePwa()`. The server snapshot is `false`, so the first
 * client render matches the server HTML and the installed-app layout settles in
 * right after hydration.
 */
export function useIsStandalonePwa(): boolean {
  return useSyncExternalStore(subscribe, isStandalonePwa, () => false);
}
