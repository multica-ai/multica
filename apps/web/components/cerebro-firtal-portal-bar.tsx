"use client";

import Script from "next/script";
import { useEffect } from "react";
import { useFlagValue } from "@multica/cerebro-feature-flags";

/**
 * Loads the Firtal portal top bar (apps.firtal.com/bar.js) on every page.
 *
 * The body reserves 48px of top padding server-side via `pt-12` in layout.tsx,
 * so the bar can never overlay app content — it always lands in reserved
 * space. When `cerebro_firtal_portal_bar` is off, this component tears down
 * any existing bar host and clears the inline body padding so the 48px is
 * reclaimed.
 *
 * The CSS-class padding on body is what makes the no-overlay guarantee work
 * with bar.js: bar.js reads `body.style.paddingTop` (inline only) and adds
 * 48px on top. Because the reservation is via CSS class, bar.js sees an empty
 * string, applies its own 48px inline, and the two values match — no double
 * padding, no layout shift.
 */
export function CerebroFirtalPortalBar() {
  const enabled = useFlagValue("cerebro_firtal_portal_bar");

  useEffect(() => {
    if (typeof document === "undefined") return;
    if (enabled) {
      document.body.dataset.firtalBar = "on";
      return;
    }
    document.body.style.paddingTop = "0";
    delete document.body.dataset.firtalBar;
    document.getElementById("firtal-bar-host")?.remove();
  }, [enabled]);

  if (!enabled) return null;

  return (
    <Script
      src="https://apps.firtal.com/bar.js"
      data-apps-url="https://apps.firtal.com/api/apps"
      data-home="https://apps.firtal.com"
      strategy="afterInteractive"
    />
  );
}
