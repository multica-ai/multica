// Pure matcher used by the service worker to decide whether a fetch should
// bypass the runtime cache entirely. Lives in its own file (instead of inline
// in sw.ts) so it can be unit-tested under jsdom without spinning up a
// ServiceWorker scope.
//
// Why this exists: a request-header check (`request.mode === "navigate"`,
// `destination === "document"`, `accept: text/html`) is not sufficient.
// Browsers also fire same-origin `<link rel="prefetch">` and Next.js
// speculation-rule prefetches as `mode: "no-cors"` with `destination: ""`
// and no `accept` header — the response is text/html but none of the
// header signals say so. Those slip past a header-only matcher and end up
// in serwist's `others` runtime cache (Tine's repro: `/login`,
// `/login?platform=desktop`, `/jeh-*/issues` cached as text/html).
//
// Instead we classify by *pathname*: anything same-origin that doesn't
// look like an asset, API call, or RSC payload is treated as a page
// document and routed to NetworkOnly. This is positive-list by exclusion,
// which is safe because the things we exclude are exhaustive (assets live
// under known prefixes or carry file extensions).

// Same-origin URL prefixes that are never page documents. The defaultCache
// rules for assets, APIs, RSC, and the Next.js data layer handle these.
const ASSET_PREFIXES: readonly string[] = [
  "/api/",
  "/_next/",
  "/uploads/",
  "/auth/",
];

// Exact same-origin paths that are not pages. Kept narrow on purpose; new
// non-page top-level routes should be added explicitly.
const ASSET_EXACT: readonly string[] = [
  "/sw.js",
  "/ws",
  "/manifest.webmanifest",
  "/robots.txt",
  "/sitemap.xml",
  "/favicon.ico",
];

// Returns true when the last segment of the URL's pathname has a file
// extension (e.g. /icons/icon-192.png, /foo.js). Used to short-circuit
// requests for static assets that live outside the known prefixes.
function hasFileExtension(pathname: string): boolean {
  const lastSegment = pathname.split("/").pop() ?? "";
  return /\.[a-z0-9]+$/i.test(lastSegment);
}

export interface NavigationMatcherArgs {
  request: Request;
  sameOrigin: boolean;
  url: URL;
}

/**
 * Returns true if the request should bypass the runtime cache because it
 * resolves to a dynamic HTML/document response.
 *
 * Excluded (defaultCache continues to handle these):
 * - cross-origin requests
 * - asset prefixes (/api/, /_next/, /uploads/, /auth/) and exact asset paths
 *   (/sw.js, /manifest.webmanifest, /robots.txt, /sitemap.xml, /favicon.ico)
 * - requests carrying the `RSC: 1` header — those are React Server Components
 *   payloads (text/x-component), not HTML; defaultCache routes them to
 *   `pages-rsc` / `pages-rsc-prefetch`
 * - any URL whose last path segment has a file extension (heuristic for
 *   static files served outside the known prefixes)
 *
 * Anything that survives those exclusions is treated as a page document —
 * regardless of `request.mode`, `destination`, or `Accept` — and routed to
 * NetworkOnly. This catches `<link rel="prefetch">`-style prefetches whose
 * header signals would otherwise look like an opaque fetch.
 */
export function isNavigationRequest(args: NavigationMatcherArgs): boolean {
  const { request, sameOrigin, url } = args;
  if (!sameOrigin) return false;
  if (request.headers.get("RSC") === "1") return false;
  const pathname = url.pathname;
  for (const prefix of ASSET_PREFIXES) {
    if (pathname.startsWith(prefix)) return false;
  }
  if (ASSET_EXACT.includes(pathname)) return false;
  if (hasFileExtension(pathname)) return false;
  return true;
}

/**
 * Names of serwist/Workbox runtime caches that historically stored HTML
 * responses for navigation requests. The activate handler in sw.ts deletes
 * these on every SW update so entries written by a pre-fix SW version do not
 * survive into the new SW's lifetime.
 *
 * Precache (`serwist-precache-v2-<origin>`) is intentionally NOT in this list
 * — that's where static JS/CSS/font assets live and must stay precached.
 */
export const LEGACY_NAVIGATION_CACHES: readonly string[] = [
  "others",
  "pages",
  "pages-rsc",
  "pages-rsc-prefetch",
];
