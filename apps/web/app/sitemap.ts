import type { MetadataRoute } from "next";
import { landingAlternates } from "@/lib/landing-locale-path";
import { createEnDict } from "@/features/landing/i18n/en";
import { isLiveRoute, siteRoutes } from "@/features/landing/site/sitemap";
import { SITE_ORIGIN } from "@/features/landing/structured-data";

type SitemapHints = Pick<MetadataRoute.Sitemap[number], "changeFrequency" | "priority">;

const HINTS: Readonly<Record<string, SitemapHints>> = {
  "/": { changeFrequency: "weekly", priority: 1.0 },
  "/about": { changeFrequency: "monthly", priority: 0.7 },
  "/changelog": { changeFrequency: "weekly", priority: 0.6 },
  "/contact-sales": { changeFrequency: "monthly", priority: 0.7 },
  "/download": { changeFrequency: "weekly", priority: 0.8 },
  "/licensing": { changeFrequency: "monthly", priority: 0.6 },
  "/privacy": { changeFrequency: "yearly", priority: 0.3 },
};

/** Pages that list recent releases change with each one. */
const RELEASE_PAGES = new Set(["/", "/changelog"]);

function latestReleaseDate(): Date | undefined {
  const dates = createEnDict().changelog.entries.map((entry) => entry.date).sort();
  const latest = dates.at(-1);
  return latest ? new Date(latest) : undefined;
}

export default function sitemap(): MetadataRoute.Sitemap {
  const releasedAt = latestReleaseDate();

  return siteRoutes()
    .filter(isLiveRoute)
    .map((route) => ({
      url: route === "/" ? SITE_ORIGIN : `${SITE_ORIGIN}${route}`,
      ...(RELEASE_PAGES.has(route) && releasedAt ? { lastModified: releasedAt } : {}),
      ...(HINTS[route] ?? { changeFrequency: "monthly", priority: 0.5 }),
      alternates: { languages: landingAlternates(route, SITE_ORIGIN) },
    }));
}
