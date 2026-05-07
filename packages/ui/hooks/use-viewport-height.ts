"use client";

import { useEffect } from "react";

/**
 * Fixes mobile browsers (especially WeChat's X5 WebView) where CSS `100vh` / `100svh`
 * doesn't match the visible viewport height.
 *
 * Sets a `--vh` CSS custom property on `<html>` equal to `window.innerHeight`,
 * updated on resize and orientation change. Use as `height: var(--vh, 100svh)`.
 */
export function useViewportHeight() {
  useEffect(() => {
    const setVh = () => {
      document.documentElement.style.setProperty("--vh", `${window.innerHeight}px`);
    };

    const handleOrientationChange = () => {
      // Wait for browser to finish re-layout after orientation switch
      setTimeout(setVh, 150);
    };

    setVh();

    window.addEventListener("resize", setVh);
    window.addEventListener("orientationchange", handleOrientationChange);

    return () => {
      window.removeEventListener("resize", setVh);
      window.removeEventListener("orientationchange", handleOrientationChange);
    };
  }, []);
}
