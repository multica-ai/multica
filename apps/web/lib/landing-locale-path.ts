import type { SupportedLocale } from "@multica/core/i18n";

/*
 * Language prefixes for the marketing site. English lives at the root
 * (`/download`); every other language the site is translated into has its own
 * prefix (`/zh/download`), which the proxy rewrites onto the same page with
 * the locale fixed. The prefixes match the docs (`/docs/zh`), and are reserved
 * as workspace slugs.
 */

/** Prefix → the locale its pages render in. */
export const LANDING_PATH_PREFIXES = { zh: "zh-Hans", ko: "ko", ja: "ja" } as const satisfies Record<
  string,
  SupportedLocale
>;

type LandingPathPrefix = keyof typeof LANDING_PATH_PREFIXES;
/** The languages the marketing site has a URL for. */
export type LandingPathLocale = "en" | (typeof LANDING_PATH_PREFIXES)[LandingPathPrefix];

const PREFIX_FOR: Record<LandingPathLocale, string> = { en: "", "zh-Hans": "zh", ko: "ko", ja: "ja" };

/** `hreflang` value (BCP 47) for each language's URL. */
export const HREFLANG: Record<LandingPathLocale, string> = { en: "en", "zh-Hans": "zh-Hans", ko: "ko", ja: "ja" };

/** First path segments served by `app/(landing)`, besides the root. */
export const LANDING_ROUTES: ReadonlySet<string> = new Set([
  "about",
  "blog",
  "careers",
  "changelog",
  "contact-sales",
  "customers",
  "download",
  "homepage",
  "licensing",
  "press",
  "pricing",
  "privacy",
  "product",
  "security",
  "solutions",
  "usecases",
]);

function firstSegment(pathname: string): string {
  return pathname.split("/")[1] ?? "";
}

function isPrefix(segment: string): segment is LandingPathPrefix {
  return Object.hasOwn(LANDING_PATH_PREFIXES, segment);
}

/** Whether an unprefixed path is a marketing page. */
export function isLandingPath(pathname: string): boolean {
  return pathname === "/" || LANDING_ROUTES.has(firstSegment(pathname));
}

/**
 * A language-prefixed marketing URL, split into its locale and the page path
 * it shows (`/zh/download` → zh-Hans, `/download`). Null when the path has no
 * prefix or the rest is not a marketing page.
 */
export function splitLandingLocale(pathname: string): { locale: LandingPathLocale; path: string } | null {
  const prefix = firstSegment(pathname);
  if (!isPrefix(prefix)) return null;
  const path = pathname.slice(prefix.length + 1) || "/";
  return isLandingPath(path) ? { locale: LANDING_PATH_PREFIXES[prefix], path } : null;
}

/** The marketing-site language a locale reads: Traditional Chinese reads the Chinese pages, untranslated locales English. */
export function landingPathLocale(locale: SupportedLocale): LandingPathLocale {
  if (locale === "zh-Hans" || locale === "zh-Hant") return "zh-Hans";
  if (locale === "ko" || locale === "ja") return locale;
  return "en";
}

/** `path` (unprefixed) in `locale`'s URL space. */
function withPrefix(locale: LandingPathLocale, path: string): string {
  const prefix = PREFIX_FOR[locale];
  if (!prefix) return path;
  return path === "/" ? `/${prefix}` : `/${prefix}${path}`;
}

/**
 * An internal href in `locale`'s URL space. Marketing pages gain the language
 * prefix; anything else — the app, docs, external and already-prefixed links —
 * is returned unchanged.
 */
export function localizeHref(locale: SupportedLocale, href: string): string {
  if (!href.startsWith("/") || href.startsWith("//")) return href;
  const cut = href.search(/[?#]/);
  const path = cut === -1 ? href : href.slice(0, cut);
  if (splitLandingLocale(path) || !isLandingPath(path)) return href;
  return withPrefix(landingPathLocale(locale), path) + (cut === -1 ? "" : href.slice(cut));
}

/** The page path behind a marketing URL, with any language prefix removed. */
export function stripLandingLocale(pathname: string): string {
  return splitLandingLocale(pathname)?.path ?? pathname;
}

/** Every language's URL for an unprefixed page, keyed by `hreflang`, with English as `x-default`. */
export function landingAlternates(path: string, origin = ""): Record<string, string> {
  const languages: Record<string, string> = {};
  for (const locale of Object.keys(PREFIX_FOR) as LandingPathLocale[]) {
    languages[HREFLANG[locale]] = origin + withPrefix(locale, path);
  }
  languages["x-default"] = origin + path;
  return languages;
}
