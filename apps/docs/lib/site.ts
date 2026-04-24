import { source } from "@/lib/source";
import { i18n } from "@/lib/i18n";

// Canonical production origin and basePath for the docs app. Used by the
// sitemap and per-page hreflang metadata — anywhere we need to construct
// absolute URLs for search engines.
export const SITE_ORIGIN = "https://www.multica.ai";
export const DOCS_BASE_PATH = "/docs";

/**
 * Build an absolute URL for a docs page from its Fumadocs-relative url
 * (e.g. "/agents" or "/zh/agents"). The home page comes through as "/",
 * which would naively serialize to ".../docs/" with a trailing slash —
 * Next serves the home at ".../docs" (no trailing), so we strip the lone
 * slash to keep the sitemap entry and the page's own canonical link byte-
 * identical. Otherwise Search Console flags a canonical mismatch.
 */
export function absoluteDocsUrl(relative: string): string {
  const path = relative === "/" ? "" : relative;
  return `${SITE_ORIGIN}${DOCS_BASE_PATH}${path}`;
}

/**
 * Build Next.js `metadata.alternates` for a docs page:
 *  - `canonical` points at the default-language version (Google consolidates
 *    ranking signals onto it)
 *  - `languages` lists every available locale under its hreflang code,
 *    plus an `x-default` fallback pointing at the canonical URL
 *
 * Slugs that only exist in one language still get a valid alternates block;
 * Google will only serve what's declared.
 */
export function docsAlternates(slugs: string[]): {
  canonical: string;
  languages: Record<string, string>;
} {
  const languages: Record<string, string> = {};
  for (const lang of i18n.languages) {
    const page = source.getPage(slugs, lang);
    if (page) languages[lang] = absoluteDocsUrl(page.url);
  }

  const canonical =
    languages[i18n.defaultLanguage] ?? Object.values(languages)[0];
  if (canonical) languages["x-default"] = canonical;

  return { canonical: canonical ?? absoluteDocsUrl("/"), languages };
}
