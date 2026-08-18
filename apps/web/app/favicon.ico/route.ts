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
export function GET() {
  return new Response(null, {
    status: 308,
    headers: { Location: "/favicon.svg" },
  });
}
