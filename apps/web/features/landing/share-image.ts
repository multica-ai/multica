/**
 * The site's share card, served from app/opengraph-image.png. A page that sets
 * its own `openGraph` replaces the inherited one whole, so it must list this
 * image again to keep it.
 */
export const SHARE_IMAGE = {
  url: "/opengraph-image.png",
  width: 1200,
  height: 630,
  alt: "Multica — People and agents, working as one team.",
} as const;
