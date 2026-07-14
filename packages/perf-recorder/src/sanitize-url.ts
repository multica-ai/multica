import type { RouteInfo } from "./types";

/**
 * Entry-time URL sanitizer. Every URL (route or resource) MUST pass through
 * this before it touches any buffer, incident, log, or state. It keeps only the
 * origin and pathname and drops username, password, query, and hash. On a parse
 * failure it returns nulls — it NEVER falls back to the raw string, so a secret
 * embedded in a malformed URL can't leak (MUL-4466 §8.3, §12).
 *
 * `base` lets relative resource URLs resolve against the current document
 * origin; timing entries expose absolute URLs already, but resource entries can
 * be relative in some engines.
 */
export function sanitizeUrl(raw: string | null | undefined, base?: string): RouteInfo {
  if (!raw) return { origin: null, pathname: null };
  let parsed: URL;
  try {
    parsed = base ? new URL(raw, base) : new URL(raw);
  } catch {
    return { origin: null, pathname: null };
  }
  // Reading `.origin`/`.pathname` off the URL object never carries query/hash
  // or credentials, so the secret-bearing parts are dropped by construction.
  const origin = parsed.origin && parsed.origin !== "null" ? parsed.origin : null;
  return {
    origin,
    pathname: parsed.pathname || null,
  };
}

/** True when the parsed URL yielded no usable location. */
export function isUrlUnavailable(route: RouteInfo): boolean {
  return route.origin === null && route.pathname === null;
}
