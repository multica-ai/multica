import { i18n } from "./i18n";

// Add the active locale prefix to root-relative MDX links so internal
// navigation inside Chinese (or any non-default-language) docs stays in
// that language. Without this, `[xx](/workspaces)` written in a `*.zh.mdx`
// renders as `<a href="/workspaces">`, which Next's basePath rewrites to
// `/docs/workspaces` and the docs middleware then routes to English —
// leaking the reader out of their chosen locale.
//
// We deliberately do NOT touch:
//   - external links (`https:`, `mailto:`, `tel:`, etc.)
//   - in-page anchors (`#section`)
//   - relative paths (`./foo`, `../bar`)
//   - paths already prefixed with a known locale
//   - the default language (URLs are intentionally prefix-less under
//     `hideLocale: 'default-locale'`)
export function prefixLocale(href: string, lang: string): string {
  if (!href) return href;
  if (lang === i18n.defaultLanguage) return href;
  if (/^[a-z][a-z0-9+.-]*:/i.test(href)) return href;
  if (href.startsWith("#")) return href;
  if (!href.startsWith("/")) return href;

  const segments = href.split("/").filter(Boolean);
  const first = segments[0];
  if (first && (i18n.languages as readonly string[]).includes(first)) {
    return href;
  }

  return href === "/" ? `/${lang}` : `/${lang}${href}`;
}
