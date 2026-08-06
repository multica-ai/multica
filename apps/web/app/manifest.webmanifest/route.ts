import type { MetadataRoute } from "next";

const manifest: MetadataRoute.Manifest = {
  id: "/",
  name: "Multica",
  short_name: "Multica",
  start_url: "/?source=pwa",
  scope: "/",
  display: "standalone",
  theme_color: "#05070b",
  background_color: "#05070b",
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
};

export function GET() {
  if (process.env.NODE_ENV !== "production") {
    return new Response(null, { status: 404 });
  }

  return Response.json(manifest, {
    headers: { "Content-Type": "application/manifest+json" },
  });
}
