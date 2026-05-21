import type { MetadataRoute } from "next";

// Web App Manifest for installable / "Add to Home Screen" PWA support.
// Apple-specific PWA meta lives in app/layout.tsx; the manifest itself
// only needs the standards-compliant fields below.
//
// Icons reference the Next.js generated routes (app/icon0.tsx, app/icon1.tsx).
// Touching the source TSX bumps the URL hash; the manifest reference stays
// stable because we use the unhashed `/icon0` form.
export default function manifest(): MetadataRoute.Manifest {
  return {
    name: "Multica",
    short_name: "Multica",
    description:
      "Project management for human + agent teams. Assign tasks, track progress, compound skills.",
    start_url: "/",
    scope: "/",
    display: "standalone",
    display_override: ["standalone", "minimal-ui", "browser"],
    orientation: "any",
    background_color: "#ffffff",
    theme_color: "#ffffff",
    categories: ["productivity", "business"],
    icons: [
      {
        src: "/icon0",
        sizes: "192x192",
        type: "image/png",
        purpose: "any",
      },
      {
        src: "/icon1",
        sizes: "512x512",
        type: "image/png",
        purpose: "any",
      },
      {
        src: "/icon2",
        sizes: "512x512",
        type: "image/png",
        purpose: "maskable",
      },
    ],
  };
}
