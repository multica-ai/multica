import { NextResponse, type NextRequest } from "next/server";
import { LOCALE_COOKIE, type SupportedLocale } from "@multica/core/i18n";
import {
  isSupportedLocale,
  MULTICA_LOCALE_HEADER,
  resolveLocaleFromSignals,
} from "./lib/locale-routing";
import {
  isLandingPath,
  landingPathLocale,
  localizeHref,
  splitLandingLocale,
} from "./lib/landing-locale-path";
import { runtimeRewriteDestination } from "./config/runtime-urls";
import { isOfficialMarketingHost } from "./lib/public-host";

// Old workspace-scoped route segments that existed before the URL refactor
// (pre-#1131). Any URL with these as the FIRST segment is a legacy URL that
// needs to be rewritten to /{slug}/{route}/... so old bookmarks, deep links,
// and post-revert-and-reapply users don't hit 404.
const LEGACY_ROUTE_SEGMENTS = new Set([
  "issues",
  "projects",
  "agents",
  "squads",
  "inbox",
  "my-issues",
  "autopilots",
  "runtimes",
  "skills",
  "settings",
  "usage",
]);

function resolveLocale(req: NextRequest): SupportedLocale {
  return resolveLocaleFromSignals({
    cookieLocale: req.cookies.get(LOCALE_COOKIE)?.value,
    acceptLanguage: req.headers.get("accept-language"),
  });
}

// Forward the resolved locale to RSC layouts via the `x-multica-locale`
// request header. layout.tsx reads it through `await headers()`. The
// `request: { headers }` form is what makes the header land on the upstream
// request — without it the value would only sit on the response.
function nextWithLocale(req: NextRequest, locale = resolveLocale(req)): NextResponse {
  const headers = new Headers(req.headers);
  headers.set(MULTICA_LOCALE_HEADER, locale);
  return NextResponse.next({ request: { headers } });
}

// The marketing site has one URL per language: English at the root, the
// others under /zh, /ko and /ja (lib/landing-locale-path). Returns null for
// paths that are not marketing pages.
function routeLandingLocale(
  req: NextRequest,
  hasSession: boolean,
  lastSlug: string | undefined,
): NextResponse | null {
  const { pathname } = req.nextUrl;
  const prefixed = splitLandingLocale(pathname);
  if (prefixed) {
    // A workspace that took a language prefix as its slug before the prefix
    // was reserved keeps its bare root for its members.
    if (prefixed.path === "/" && hasSession && lastSlug === pathname.slice(1)) return null;
    const url = req.nextUrl.clone();
    url.pathname = prefixed.path;
    const headers = new Headers(req.headers);
    headers.set(MULTICA_LOCALE_HEADER, prefixed.locale);
    return NextResponse.rewrite(url, { request: { headers } });
  }
  if (!isLandingPath(pathname)) return null;

  // A remembered language choice goes to that language's URL. Without one the
  // root URL stays English — the same page crawlers index — and the page
  // offers the browser's language rather than switching to it.
  const cookie = req.cookies.get(LOCALE_COOKIE)?.value ?? null;
  const chosen = isSupportedLocale(cookie) ? landingPathLocale(cookie) : "en";
  if (chosen !== "en") {
    const url = req.nextUrl.clone();
    url.pathname = localizeHref(chosen, pathname);
    return NextResponse.redirect(url);
  }
  const resolved = resolveLocale(req);
  return nextWithLocale(req, landingPathLocale(resolved) === "en" ? resolved : "en");
}

// Next.js 16 renamed `middleware` → `proxy`. API surface (NextRequest /
// NextResponse / cookies / matcher) is identical; the only behavioral
// change is the runtime — proxy is forced to nodejs and cannot opt into
// edge.
export function proxy(req: NextRequest) {
  const { pathname } = req.nextUrl;
  const runtimeDestination = runtimeRewriteDestination(pathname, process.env);
  if (runtimeDestination) {
    const url = new URL(runtimeDestination);
    url.search = req.nextUrl.search;
    return NextResponse.rewrite(url);
  }

  const hasSession = req.cookies.has("multica_logged_in");
  const lastSlug = req.cookies.get("last_workspace_slug")?.value;

  // --- Legacy URL redirect: /issues/... → /{slug}/issues/... ---
  // Old bookmarks and clients that hit us before the slug migration would
  // otherwise 404 since the route moved under [workspaceSlug].
  const firstSegment = pathname.split("/")[1] ?? "";
  if (LEGACY_ROUTE_SEGMENTS.has(firstSegment)) {
    const url = req.nextUrl.clone();

    if (!hasSession) {
      url.pathname = "/login";
      return NextResponse.redirect(url);
    }

    if (lastSlug) {
      // Preserve deep-link path + query: /issues/abc → /{lastSlug}/issues/abc
      url.pathname = `/${lastSlug}${pathname}`;
      return NextResponse.redirect(url);
    }

    // Logged-in but no cookie yet (never opened a workspace, or the cookie was
    // cleared). Root is the wrong destination: the root-path rule below leaves
    // `/` on the public site for the official marketing hosts even with a
    // session, so bouncing there dead-ends on the landing page instead of
    // reaching the app. /login already resolves an authenticated visitor
    // against their workspace list — including pending invitations and the
    // no-workspace-yet case — and replaces to the right destination. Deep-link
    // path and query are dropped rather than passed as `next`: they are legacy
    // segments themselves, so feeding one back would land here again.
    url.pathname = "/login";
    url.search = "";
    return NextResponse.redirect(url);
  }

  // --- Root path: redirect logged-in users to their last workspace ---
  // The official cloud host also serves the public marketing site. Visiting
  // https://multica.ai/ must remain a public-site navigation even when a local
  // desktop/runtime session has fresh auth cookies; explicit app routes such
  // as /acme/issues and legacy /issues still route to the workspace app.
  if (
    pathname === "/" &&
    hasSession &&
    lastSlug &&
    !isOfficialMarketingHost(req.nextUrl.hostname)
  ) {
    const url = req.nextUrl.clone();
    url.pathname = `/${lastSlug}/issues`;
    return NextResponse.redirect(url);
  }

  const landing = routeLandingLocale(req, hasSession, lastSlug);
  if (landing) return landing;

  // --- Default: forward locale header to RSC, no redirect/rewrite ---
  // Covers logged-out root path, /login, /:slug/*, and everything else.
  return nextWithLocale(req);
}

export const config = {
  // i18n header must land on every page request, so we use the standard
  // negative-lookahead pattern from Next's i18n guide, plus explicit runtime
  // proxy routes whose upstream origins are resolved from process.env at
  // request time instead of being baked into next.config.js at build time.
  matcher: [
    "/v1/:path*",
    // Uploads stream through an App Router handler instead of Proxy's body clone.
    "/api",
    "/api/((?!upload-file(?:/|$)).*)",
    "/auth/:path*",
    "/docs/:path*",
    "/ws",
    "/((?!api|v1|_next/static|_next/image|favicon.ico|.*\\.).*)",
  ],
};
