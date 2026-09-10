/**
 * Legacy `/favicon.ico` requests point at the real SVG favicon.
 *
 * The `Location` is deliberately relative. `Response.redirect()` only accepts
 * an absolute URL, which forces resolving against `request.url` — and on a
 * self-hosted deployment that carries the server's own listen address, not the
 * public origin. `Dockerfile.web` sets `HOSTNAME=0.0.0.0` / `PORT=3000`, and a
 * plain `next start` defaults to the same, so the redirect came back as
 * `http://0.0.0.0:3000/favicon.svg`: unreachable for every client, and a leak
 * of the internal bind address. A relative `Location` is valid per RFC 7231
 * §7.1.2 and resolves against whatever origin the client actually used, so it
 * needs no trust in `Host` / `X-Forwarded-Host`.
 */

/**
 * Keep the handler off the prerender cache path.
 *
 * Reading `request.url` used to mark this route dynamic implicitly. Dropping
 * that argument left nothing dynamic for Next to detect, so it treated the
 * route as cacheable and tried to store the response — whose body is zero
 * bytes, which the prerender LRU rejects outright:
 *
 *   Failed to update prerender cache for /favicon.ico
 *   Error: LRUCache: calculateSize returned 0, but size must be > 0.
 *
 * Every browser requests `/favicon.ico` unprompted, so that is one error line
 * per request in every deployment, against a cache write that can never
 * succeed. Forcing dynamic restores the per-request semantics the old
 * `request.url` read gave us by accident, without reintroducing the absolute
 * URL. Do not remove this alongside a "the handler takes no arguments" tidy-up.
 */
export const dynamic = "force-dynamic";

export function GET() {
  return new Response(null, {
    status: 308,
    headers: { Location: "/favicon.svg" },
  });
}
