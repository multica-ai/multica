import type { SupportedLocale } from "@multica/core/i18n";

export function docsHrefForLocale(locale: SupportedLocale): string {
  if (locale === "zh-Hans" || locale === "zh-Hant") return "/docs/zh";
  if (locale === "ko") return "/docs/ko";
  if (locale === "ja") return "/docs/ja";
  if (locale === "fr") return "/docs/fr";
  return "/docs";
}

// The docs site is a separate Next.js app served under /docs (see the
// beforeFiles rewrites in apps/web/next.config.ts). Navigating there leaves
// this app, so links to it must not go through next/link: prefetching makes
// this app's router fetch the docs app's RSC payload and then request the
// docs app's chunks from this app's /_next/static, which 404s.
export function isDocsHref(href: string): boolean {
  return href === "/docs" || href.startsWith("/docs/");
}
