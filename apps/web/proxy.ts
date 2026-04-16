import { NextResponse, type NextRequest } from "next/server";

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

// Next.js 16 renamed `middleware` → `proxy`. The runtime API is identical.
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
  if (pathname === "/") {
    if (!hasSession) return NextResponse.next();

    if (lastSlug) {
      const url = req.nextUrl.clone();
      url.pathname = `/${lastSlug}/issues`;
      return NextResponse.redirect(url);
    }

    // No last_workspace_slug cookie → let landing page pick the first workspace
    // client-side (features/landing/components/redirect-if-authenticated.tsx).
    return NextResponse.next();
  }

  return NextResponse.next();
}

export const config = {
  matcher: [
    "/",
    "/issues/:path*",
    "/projects/:path*",
    "/agents/:path*",
    "/inbox/:path*",
    "/my-issues/:path*",
    "/autopilots/:path*",
    "/runtimes/:path*",
    "/skills/:path*",
    "/settings/:path*",
  ],
};
