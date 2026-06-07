"use client";

import Script from "next/script";
import { useEffect, useLayoutEffect } from "react";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * Loads the Firtal portal top bar (apps.firtal.com/bar.js) on every page.
 *
 * bar.js owns the body padding needed for normal document flow and manages the
 * data-firtal-bar attribute ("visible" | "hidden") to reflect collapse state.
 * Cerebro tracks load/error so fixed-position chrome (the workspace sidebar)
 * can move below the bar once it has actually loaded. When the flag is off, or
 * when bar.js fails to load in an installed PWA, stale inline padding from an
 * earlier document is removed instead of pushing the whole app shell down.
 */
export function CerebroFirtalPortalBar() {
  const enabled = useFlagValue("cerebro_firtal_portal_bar");

  // Remove stale bar state from a previous PWA document on the way in.
  useLayoutEffect(() => {
    if (typeof document === "undefined") return;
    if (!enabled) return;
    delete document.body.dataset.firtalBar;
    document.body.style.paddingTop = "";
  }, [enabled]);

  // Clean up when the flag is toggled off.
  useEffect(() => {
    if (typeof document === "undefined") return;
    if (enabled) return;
    document.body.style.paddingTop = "";
    delete document.body.dataset.firtalBar;
    document.getElementById("firtal-bar-host")?.remove();
  }, [enabled]);

  if (!enabled) return null;

  const markBarLoaded = () => {
    // Set "visible" so the CSS sidebar offset applies immediately; bar.js
    // will override to "hidden" if the user had previously collapsed the bar.
    document.body.dataset.firtalBar = "visible";
  };

  const markBarUnavailable = () => {
    document.body.style.paddingTop = "";
    delete document.body.dataset.firtalBar;
    document.getElementById("firtal-bar-host")?.remove();
  };

  return (
    <Script
      src="https://apps.firtal.com/bar.js"
      data-apps-url="https://apps.firtal.com/api/apps"
      data-home="https://apps.firtal.com"
      strategy="afterInteractive"
      onLoad={markBarLoaded}
      onReady={markBarLoaded}
      onError={markBarUnavailable}
    />
  );
}
