import { NextResponse, type NextRequest } from "next/server";
import { i18n } from "@/lib/i18n";

const BASE_PATH = "/docs";

// Self-contained i18n middleware. We don't use fumadocs-core's built-in
// middleware because it isn't basePath-aware — both its rewrite targets
// and redirect Location headers are built from the basePath-stripped path,
// leaving URLs like `/en/agents` or `/` that Next then fails to resolve
// inside a basePath-mounted app. Logic mirrors fumadocs's default-locale
// flavor: hide `/en` prefix for the default language, keep `/zh` prefix
// for other languages.
export default function middleware(request: NextRequest) {
  const pathname = request.nextUrl.pathname;
  const pathLocale = i18n.languages.find(
    (loc) => pathname === `/${loc}` || pathname.startsWith(`/${loc}/`),
  );

  if (!pathLocale) {
    // No locale in URL → rewrite to the default-language route. Build the
    // target from `request.url` (which includes basePath); `new URL(path,
    // base)` replaces only the pathname, so we emit the full prefixed path
    // once and Next does not double-add basePath.
    const target = `${BASE_PATH}/${i18n.defaultLanguage}${pathname === "/" ? "" : pathname}`;
    return NextResponse.rewrite(new URL(target, request.url));
  }

  if (pathLocale === i18n.defaultLanguage) {
    // Explicit default-language prefix → strip it so the canonical URL
    // is prefix-less. Use the same `new URL(target, request.url)` pattern
    // as the rewrite branch above, then explicitly carry the search string
    // through — otherwise marketing UTMs / referral params silently
    // disappear on the locale strip.
    const stripped = pathname.slice(`/${pathLocale}`.length);
    const target = `${BASE_PATH}${stripped || "/"}`;
    const url = new URL(target, request.url);
    url.search = request.nextUrl.search;
    return NextResponse.redirect(url);
  }

  // Non-default locale in URL → let it through; Next matches on the
  // `[lang]` segment directly.
  return NextResponse.next();
}

export const config = {
  // Run on every path except static/api and root metadata routes. Includes
  // the bare `/` root so `/docs/` lands on the English home. `sitemap.xml`
  // and `robots.txt` MUST be excluded — they're not under `[lang]/`, so
  // routing them through the locale rewrite would 404 the sitemap that
  // robots.txt advertises to crawlers.
  matcher: [
    "/((?!api|_next/static|_next/image|favicon.ico|sitemap.xml|robots.txt).*)",
    "/",
  ],
};
