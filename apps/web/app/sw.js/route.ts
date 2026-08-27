import { staticAssetWorkerSource } from "@/lib/static-sw-policy";

/**
 * Serve `/sw.js` from a Route Handler instead of `public/`.
 *
 * The body is generated from `static-sw-policy.ts` so the fetch matchers have
 * one source of truth. `force-dynamic` plus `Cache-Control: no-cache` keep
 * browsers from pinning an old worker across a k8s SHA deploy — hashed
 * `/_next/static` files are safe to cache-first; product HTML is network-first
 * with an offline fallback. The worker script itself is not cacheable.
 * See `apps/web/app/favicon.ico/route.ts` for why a no-arg GET without
 * `force-dynamic` ends up on Next's prerender cache path.
 */
export const dynamic = "force-dynamic";

export function GET() {
  return new Response(staticAssetWorkerSource(), {
    headers: {
      "Content-Type": "text/javascript; charset=utf-8",
      "Cache-Control": "no-cache",
      "Service-Worker-Allowed": "/",
    },
  });
}
