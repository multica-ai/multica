// Redirect the legacy /favicon.ico request to the real SVG icon.
//
// The Location is deliberately RELATIVE. Building an absolute URL from
// request.url bakes in the standalone server's bind address — behind a
// reverse proxy that emitted `Location: http://0.0.0.0:3000/favicon.svg`,
// a dead end for every browser and a leak of the internal bind address
// (GH #7116). A relative Location resolves against whatever origin the
// client actually used, which is the only origin we can trust here:
// self-hosted deployments sit behind arbitrary proxies and need no
// public-URL configuration for this to hold.
export function GET() {
  return new Response(null, {
    status: 308,
    headers: { Location: "/favicon.svg" },
  });
}
