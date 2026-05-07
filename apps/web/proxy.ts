import { NextResponse, type NextRequest } from "next/server";
import { matchLocale, LOCALE_COOKIE } from "@multica/core/i18n";

// Old workspace-scoped route segments that existed before the URL refactor
// (pre-#1131). Any URL with these as the FIRST segment is a legacy URL that
// needs to be rewritten to /{slug}/{route}/... so old bookmarks, deep links,
// and post-revert-and-reapply users don't hit 404.
const LEGACY_ROUTE_SEGMENTS = new Set([
  "issues",
  "projects",
  "agents",
  "inbox",
  "my-issues",
  "autopilots",
  "runtimes",
  "skills",
  "settings",
]);

// Resolve the active locale per request. Cookie wins over Accept-Language;
// matchLocale() falls back to DEFAULT_LOCALE when neither yields a match.
function resolveLocale(req: NextRequest): string {
  const cookieLocale = req.cookies.get(LOCALE_COOKIE)?.value;
  const acceptLanguage = req.headers.get("accept-language") ?? "";
  const candidates: string[] = [];
  if (cookieLocale) candidates.push(cookieLocale);
  for (const part of acceptLanguage.split(",")) {
    const tag = part.split(";")[0]?.trim();
    if (tag) candidates.push(tag);
  }
  return matchLocale(candidates);
}

// Forward the resolved locale to RSC layouts via the `x-multica-locale`
// request header. layout.tsx reads it through `await headers()`. The
// `request: { headers }` form is what makes the header land on the upstream
// request — without it the value would only sit on the response.
function nextWithLocale(req: NextRequest): NextResponse {
  const headers = new Headers(req.headers);
  headers.set("x-multica-locale", resolveLocale(req));
  return NextResponse.next({ request: { headers } });
}

// Next.js 16 renamed `middleware` → `proxy`. API surface (NextRequest /
// NextResponse / cookies / matcher) is identical; the only behavioral
// change is the runtime — proxy is forced to nodejs and cannot opt into
// edge.
export function proxy(req: NextRequest) {
  const { pathname } = req.nextUrl;
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

    // Logged-in but no cookie yet (first login since slug migration, or
    // cookie cleared). Bounce to root; the root-path logic below picks a
    // workspace and writes the cookie, then future hits short-circuit here.
    url.pathname = "/";
    return NextResponse.redirect(url);
  }

  // --- Root path: redirect logged-in users to their last workspace ---
  if (pathname === "/" && hasSession && lastSlug) {
    const url = req.nextUrl.clone();
    url.pathname = `/${lastSlug}/issues`;
    return NextResponse.redirect(url);
  }

  // --- Default: forward locale header to RSC, no redirect/rewrite ---
  // Covers logged-out root path, /login, /:slug/*, and everything else.
  return nextWithLocale(req);
}

export const config = {
  // i18n header must land on every page request, so we use the standard
  // negative-lookahead pattern from Next's i18n guide: skip API routes
  // (Go backend), Next internals, and any path with a file extension
  // (favicons, sw.js, public/* assets).
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|.*\\.).*)"],
};
