import type { MetadataRoute } from "next";

// Web App Manifest, served by Next's metadata route at `/manifest.webmanifest`.
// Next also auto-injects `<link rel="manifest">` into <head>, so no manual tag
// is needed in layout.tsx.
//
// These fields define Multica's PWA install contract. Modern Chromium promotes
// installation from a valid manifest served over a secure origin (name /
// short_name, start_url, scope, display: standalone, and 192px + 512px icons).
// The actual prompt is additionally gated by browser engagement heuristics, so
// treat this as the baseline we guarantee rather than a hard on/off switch.
// `id` is pinned so future `start_url` tweaks are not mistaken for a new app.
//
// `theme_color` cannot be media-query responsive in a manifest (unlike the
// `viewport.themeColor` in layout.tsx). We use the light background color; the
// installed title bar is therefore light in both color schemes — an accepted,
// purely cosmetic trade-off for the install surface.
export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/",
    name: "Multica",
    short_name: "Multica",
    description:
      "Open-source platform that turns coding agents into real teammates.",
    // The manifest copy above is authored in English.
    lang: "en",
    dir: "ltr",
    start_url: "/",
    scope: "/",
    display: "standalone",
    background_color: "#ffffff",
    theme_color: "#ffffff",
    icons: [
      { src: "/icons/icon-192.png", sizes: "192x192", type: "image/png", purpose: "any" },
      { src: "/icons/icon-512.png", sizes: "512x512", type: "image/png", purpose: "any" },
      { src: "/icons/icon-maskable-192.png", sizes: "192x192", type: "image/png", purpose: "maskable" },
      { src: "/icons/icon-maskable-512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
    ],
  };
}
