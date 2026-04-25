/* global self, caches, fetch, URL, Response */
// Multica PWA service worker.
// Strategy:
//   - Never touch API / WebSocket / auth / uploads — those are auth-scoped and dynamic.
//   - Cache-first for hashed Next.js build assets (/_next/static/*) and /icons/*.
//   - Network-first for navigations, falling back to /offline when the network is down.
//   - Bypass everything else.
//
// On logout the app clears caches via `caches.keys()` so per-user data does not leak
// between sessions on the same device.

const VERSION = "v1";
const STATIC_CACHE = `multica-static-${VERSION}`;
const SHELL_CACHE = `multica-shell-${VERSION}`;
const OFFLINE_URL = "/offline";

const NEVER_CACHE_PREFIXES = ["/api/", "/ws", "/auth/", "/uploads/"];

self.addEventListener("install", (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE).then((cache) => cache.add(OFFLINE_URL)).catch(() => {})
  );
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  event.waitUntil(
    (async () => {
      const keys = await caches.keys();
      await Promise.all(
        keys
          .filter((k) => k !== STATIC_CACHE && k !== SHELL_CACHE)
          .map((k) => caches.delete(k))
      );
      await self.clients.claim();
    })()
  );
});

self.addEventListener("message", (event) => {
  if (event.data === "SKIP_WAITING") self.skipWaiting();
  if (event.data === "CLEAR_CACHES") {
    event.waitUntil(
      caches.keys().then((keys) => Promise.all(keys.map((k) => caches.delete(k))))
    );
  }
});

function shouldBypass(url) {
  if (url.origin !== self.location.origin) return true;
  return NEVER_CACHE_PREFIXES.some((p) => url.pathname.startsWith(p));
}

self.addEventListener("fetch", (event) => {
  const req = event.request;
  if (req.method !== "GET") return;

  const url = new URL(req.url);
  if (shouldBypass(url)) return;

  // Hashed static build output: cache-first, immutable.
  if (url.pathname.startsWith("/_next/static/") || url.pathname.startsWith("/icons/")) {
    event.respondWith(
      caches.open(STATIC_CACHE).then(async (cache) => {
        const hit = await cache.match(req);
        if (hit) return hit;
        const res = await fetch(req);
        if (res.ok) cache.put(req, res.clone());
        return res;
      })
    );
    return;
  }

  // HTML navigations: network-first, fallback to offline page.
  if (req.mode === "navigate") {
    event.respondWith(
      (async () => {
        try {
          return await fetch(req);
        } catch {
          const cache = await caches.open(SHELL_CACHE);
          const offline = await cache.match(OFFLINE_URL);
          return offline ?? new Response("Offline", { status: 503 });
        }
      })()
    );
    return;
  }
});
