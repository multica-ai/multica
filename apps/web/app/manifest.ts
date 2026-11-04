import type { MetadataRoute } from "next";

/**
 * Installable web manifest. Start at /inbox so session-aware proxy routing
 * opens the workspace or login; the marketing host's root would open the landing
 * page even for a signed-in user.
 *
 * Standard icons share desktop artwork. The maskable SVG supplies full-bleed
 * artwork for launcher cropping; regenerate raster sizes from those source assets.
 */

/** Launch path. Exported so manifest.test.ts can run it through the proxy. */
export const PWA_START_URL = "/inbox";

export default function manifest(): MetadataRoute.Manifest {
  return {
    id: "/",
    name: "Multica",
    short_name: "Multica",
    description:
      "Assign tasks to coding agents, track progress, and keep your team's work in one place.",
    start_url: PWA_START_URL,
    scope: "/",
    display: "standalone",
    // Splash-screen colours. The runtime status bar is driven by the
    // per-scheme `<meta name="theme-color">` pair in app/layout.tsx, which the
    // manifest cannot express — these are the pre-launch fallback only.
    background_color: "#ffffff",
    theme_color: "#ffffff",
    categories: ["productivity"],
    icons: [
      {
        src: "/icons/icon-192.png",
        sizes: "192x192",
        type: "image/png",
        purpose: "any",
      },
      {
        src: "/icons/icon-512.png",
        sizes: "512x512",
        type: "image/png",
        purpose: "any",
      },
      {
        src: "/icons/icon-maskable-512.png",
        sizes: "512x512",
        type: "image/png",
        purpose: "maskable",
      },
    ],
    // Long-press actions on the installed launcher icon. Both are legacy
    // segments, so they resolve to the last workspace the same way start_url
    // does instead of needing a slug the manifest cannot know.
    shortcuts: [
      { name: "Inbox", url: "/inbox" },
      { name: "My Issues", url: "/my-issues" },
    ],
  };
}
