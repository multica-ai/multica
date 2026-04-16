import { NextResponse, type NextRequest } from "next/server";

// Paths that should not be touched by proxy (assets, API, auth callbacks).
// NOTE: `matcher` below already constrains this to "/", so in practice this
// list is informational — kept for clarity and to mirror the intended
// redirect boundary if the matcher is ever broadened.
// PUBLIC_PREFIXES extends paths.ts:GLOBAL_PREFIXES with hosting-asset paths
// (/_next, /favicon, /robots.txt, etc.). Auth-flow subset must stay in sync.
const PUBLIC_PREFIXES = [
  "/_next",
  "/api",
  "/auth/",
  "/login",
  "/signup",
  "/onboarding",
  "/invite/",
  "/favicon",
  "/robots.txt",
  "/sitemap.xml",
  "/manifest.json",
];

function isPublic(pathname: string): boolean {
  return PUBLIC_PREFIXES.some((p) => pathname === p || pathname.startsWith(p));
}

// Next.js 16 renamed `middleware` → `proxy`. The runtime API is identical.
export function proxy(req: NextRequest) {
  const { pathname } = req.nextUrl;

  // Only handle root path
  if (pathname !== "/") return NextResponse.next();
  if (isPublic(pathname)) return NextResponse.next();

  // Check the "is logged in" cookie. Name comes from
  // apps/web/features/auth/auth-cookie.ts (setLoggedInCookie).
  const hasSession = req.cookies.has("multica_logged_in");
  if (!hasSession) return NextResponse.next();

  // Logged-in user at root: redirect to last workspace's issues page.
  const lastSlug = req.cookies.get("last_workspace_slug")?.value;
  if (lastSlug) {
    const url = req.nextUrl.clone();
    url.pathname = `/${lastSlug}/issues`;
    return NextResponse.redirect(url);
  }

  // No last_workspace_slug cookie yet → first login → let landing page handle
  // (Task 10's client-side redirect picks the first workspace).
  return NextResponse.next();
}

export const config = {
  matcher: ["/"],
};
