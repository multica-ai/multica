import type { MetadataRoute } from "next";

/** Signed-in app routes; crawlers have nothing public to read there. */
const PRIVATE_ROUTES = [
  "/api/",
  "/ws",
  "/auth/",
  "/issues",
  "/board",
  "/inbox",
  "/agents",
  "/settings",
  "/my-issues",
  "/runtimes",
  "/skills",
];

/**
 * AI search and assistant crawlers, welcomed by name. A crawler that matches
 * its own group ignores the `*` group, so each group repeats the private
 * routes.
 */
const AI_CRAWLERS = [
  "ClaudeBot",
  "Claude-User",
  "Claude-SearchBot",
  "OAI-SearchBot",
  "ChatGPT-User",
  "PerplexityBot",
  "Perplexity-User",
  "Google-Extended",
];

export default function robots(): MetadataRoute.Robots {
  const baseUrl = "https://www.multica.ai";

  return {
    rules: [
      {
        userAgent: "*",
        allow: ["/", "/about", "/changelog"],
        disallow: PRIVATE_ROUTES,
      },
      {
        userAgent: AI_CRAWLERS,
        allow: ["/", "/docs/", "/llms.txt"],
        disallow: PRIVATE_ROUTES,
      },
    ],
    sitemap: [`${baseUrl}/sitemap.xml`, `${baseUrl}/docs/sitemap.xml`],
  };
}
