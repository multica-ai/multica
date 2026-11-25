import { NextResponse, type NextFetchEvent, type NextRequest } from "next/server";
import {
  buildAiVisitRecord,
  prefersMarkdown,
  reportAiVisit,
  type AiVisitFormat,
} from "@multica/core/ai-traffic";
import { i18n } from "@/lib/i18n";

const BASE_PATH = "/docs";
const MARKDOWN_SUFFIX = /\.mdx?$/;
const MARKDOWN_FILES = new Set(["llms.txt", "llms-full.txt"]);

/**
 * The Markdown route for a page requested as Markdown, either by URL
 * (`/agents.md`, `/zh/agents.md`, `/index.md`) or by an `Accept` header that
 * prefers Markdown. Returns null for every other request, including files
 * such as `llms.txt`.
 */
export function markdownRewritePath(pathname: string, acceptsMarkdown: boolean): string | null {
  const bySuffix = MARKDOWN_SUFFIX.test(pathname);
  if (!bySuffix && (!acceptsMarkdown || pathname.split("/").at(-1)?.includes("."))) return null;

  const segments = pathname.replace(MARKDOWN_SUFFIX, "").split("/").filter(Boolean);
  const lang = (i18n.languages as readonly string[]).includes(segments[0] ?? "")
    ? segments.shift()!
    : i18n.defaultLanguage;
  if (bySuffix && segments.at(-1) === "index") segments.pop();
  return `${BASE_PATH}/${lang}/llms.mdx${segments.map((segment) => `/${segment}`).join("")}`;
}

// Self-contained i18n proxy. We don't use fumadocs-core's built-in
// middleware because it isn't basePath-aware — both its rewrite targets
// and redirect Location headers are built from the basePath-stripped path,
// leaving URLs like `/en/agents` or `/` that Next then fails to resolve
// inside a basePath-mounted app. Logic mirrors fumadocs's default-locale
// flavor: hide `/en` prefix for the default language, keep `/zh` prefix
// for other languages.
function routeLocale(request: NextRequest): NextResponse {
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

export function proxy(request: NextRequest, event?: NextFetchEvent) {
  const pathname = request.nextUrl.pathname;
  const markdownPath = markdownRewritePath(pathname, prefersMarkdown(request.headers.get("accept")));
  const response = markdownPath
    ? NextResponse.rewrite(new URL(markdownPath, request.url))
    : routeLocale(request);

  const format: AiVisitFormat =
    markdownPath || MARKDOWN_FILES.has(pathname.split("/").at(-1) ?? "") ? "markdown" : "html";
  reportAiVisit(
    buildAiVisitRecord({
      app: "docs",
      method: request.method,
      pathname: `${BASE_PATH}${pathname === "/" ? "" : pathname}`,
      headers: request.headers,
      format,
      status: response.headers.has("location") ? response.status : null,
    }),
    { env: process.env, waitUntil: event && ((promise) => event.waitUntil(promise)) },
  );
  return response;
}

export const config = {
  // Run on every path except static/api and root metadata routes. Includes
  // the bare `/` root so `/docs/` lands on the English home. Metadata routes
  // such as `icon.svg` are not under `[lang]/` and must bypass locale routing
  // so browsers and crawlers can fetch the original resources.
  matcher: [
    "/((?!api|_next/static|_next/image|images|videos|favicon.ico|icon\\.svg$|sitemap.xml|robots.txt).*)",
    "/",
  ],
};
