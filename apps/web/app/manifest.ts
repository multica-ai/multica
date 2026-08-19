import type { MetadataRoute } from "next";

/**
 * Web app manifest — what makes the mobile web app installable and lets it run
 * in its own window instead of a browser tab.
 *
 * `start_url` is deliberately NOT "/". The official marketing hosts keep the
 * root path on the public site even for a signed-in session (see
 * `isOfficialMarketingHost` in proxy.ts), so an installed app pointed at "/"
 * would open the landing page. "/inbox" is one of `LEGACY_ROUTE_SEGMENTS`,
 * which proxy.ts resolves per session: signed in with a known workspace it
 * lands on that workspace's inbox, signed in without one it lands on /login
 * (which resolves against the workspace list), and signed out it lands on
 * /login too. All three are pinned in manifest.test.ts, because a launcher
 * icon has no URL bar to recover from a wrong destination.
 *
 * The icons under /icons are generated, not hand-drawn. To regenerate after a
 * brand change, edit public/icons/icon.svg and run from public/icons:
 *
 *   sips -s format png --resampleHeightWidth 512 512 icon.svg --out icon-maskable-512.png
 *   sips -s format png --resampleHeightWidth 180 180 icon.svg --out apple-touch-icon.png
 *   sips -s format png --resampleHeightWidth 512 512 ../../../desktop/build/icon.png --out icon-512.png
 *   sips -s format png --resampleHeightWidth 192 192 ../../../desktop/build/icon.png --out icon-192.png
 *
 * The two `any` icons come from the desktop app icon so an installed web app
 * and an installed desktop app show the same artwork; the maskable one is
 * full-bleed because Android crops it to the launcher's shape.
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
