"use client";

import Script from "next/script";
import { useEffect, useLayoutEffect } from "react";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * Loads the Firtal portal top bar (apps.firtal.com/bar.js) on every page.
 *
 * bar.js owns the body padding needed for normal document flow. Cerebro only
 * tracks whether the bar actually loaded so fixed-position Multica chrome
 * (notably the workspace sidebar) can move below it. When the flag is off, or
 * when bar.js fails to load in an installed PWA, stale inline padding from an
 * earlier document is removed instead of pushing the whole app shell down.
 */
export function CerebroFirtalPortalBar() {
  const enabled = useFlagValue("cerebro_firtal_portal_bar");

  useLayoutEffect(() => {
    if (typeof document === "undefined") return;
    if (!enabled) return;
    delete document.body.dataset.firtalBar;
    document.body.style.paddingTop = "";
  }, [enabled]);

  useEffect(() => {
    if (typeof document === "undefined") return;
    if (enabled) return;
    document.body.style.paddingTop = "0";
    delete document.body.dataset.firtalBar;
    document.getElementById("firtal-bar-host")?.remove();
  }, [enabled]);

  if (!enabled) return null;

  const markBarLoaded = () => {
    document.body.dataset.firtalBar = "on";
  };

  const markBarUnavailable = () => {
    document.body.style.paddingTop = "0";
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
