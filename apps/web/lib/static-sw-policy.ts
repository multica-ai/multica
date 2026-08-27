/**
 * Service-worker policy for the compact web / installed PWA.
 *
 * Static assets are first: hashed `/_next/static/*` is cache-first (immutable
 * filenames survive a k8s SHA deploy), launcher icons are stale-while-revalidate.
 * There is no giant precache of JS — that would slow the first visit the PWA
 * is trying to feel fast on. Icons are small enough to warm on install.
 *
 * Product documents are network-first with an offline fallback. Cookie auth
 * makes HTML personal, but a phone PWA is a single-user device; serving the
 * last inbox shell offline is the difference between a white screen and a
 * usable (if stale) app. Marketing `/` and RSC flight requests stay
 * network-only so a deploy cannot pin a stale landing page or a flight
 * payload that points at deleted chunks.
 *
 * The worker script is generated from this module (`staticAssetWorkerSource`)
 * so the matchers have one source of truth. Bump the cache names when the
 * matcher set changes so activate() drops the previous Cache Storage buckets.
 */

export const STATIC_CACHE_NAME = "multica-static-v2";
export const PAGE_CACHE_NAME = "multica-pages-v2";
/** @deprecated use STATIC_CACHE_NAME — kept so older tests/comments still grep. */
export const CACHE_NAME = STATIC_CACHE_NAME;

export const LAST_DOCUMENT_PATH = "/__pwa-last-document";

export const PRECACHE_PATHS = [
  "/icons/icon-192.png",
  "/icons/icon-512.png",
  "/icons/icon-maskable-512.png",
  "/icons/apple-touch-icon.png",
  "/favicon.svg",
] as const;

export const HASHED_STATIC_PATH = /^\/_next\/static\//;
export const REVALIDATE_PATH = /^\/(icons\/|favicon\.svg$)/;
export const BYPASS_PATH = /^\/(v1|api|auth|ws|uploads)(\/|$)/;
export const MARKETING_PATH =
  /^\/(homepage|download|changelog|usecases|contact-sales|about|docs)(\/|$)/;
export const PWA_START_PATH = /^\/(inbox|my-issues)$/;

/** Combined matcher for tests that still talk about "static assets". */
export const STATIC_PATH = /^\/(_next\/static\/|icons\/|favicon\.svg$)/;

export type StaticSwAction =
  | "cache-first"
  | "stale-revalidate"
  | "network-first-document"
  | "ignore";

export type StaticSwRequest = {
  method: string;
  url: string;
  pageOrigin: string;
  mode?: string;
  destination?: string;
  headers?: { get(name: string): string | null };
};

function parseUrl(url: string): URL | null {
  try {
    return new URL(url);
  } catch {
    return null;
  }
}

function isFlight(input: StaticSwRequest, url: URL): boolean {
  const headers = input.headers;
  if (headers?.get("RSC") === "1") return true;
  if (headers?.get("Next-Router-Prefetch") != null) return true;
  return url.searchParams.has("_rsc");
}

function isDocument(input: StaticSwRequest): boolean {
  return input.mode === "navigate" || input.destination === "document";
}

function isMarketingPath(pathname: string): boolean {
  return pathname === "/" || MARKETING_PATH.test(pathname);
}

export function staticSwAction(input: StaticSwRequest): StaticSwAction {
  if (input.method !== "GET") return "ignore";
  const url = parseUrl(input.url);
  if (!url) return "ignore";
  if (url.origin !== input.pageOrigin) return "ignore";
  if (BYPASS_PATH.test(url.pathname)) return "ignore";
  if (url.pathname === LAST_DOCUMENT_PATH) return "ignore";
  if (isFlight(input, url)) return "ignore";

  if (isDocument(input)) {
    if (isMarketingPath(url.pathname)) return "ignore";
    if (HASHED_STATIC_PATH.test(url.pathname)) return "ignore";
    return "network-first-document";
  }

  if (HASHED_STATIC_PATH.test(url.pathname)) return "cache-first";
  if (REVALIDATE_PATH.test(url.pathname)) return "stale-revalidate";
  return "ignore";
}

/** True when the worker will answer from Cache Storage without waiting on the network. */
export function shouldCacheStatically(input: StaticSwRequest): boolean {
  const action = staticSwAction(input);
  return action === "cache-first" || action === "stale-revalidate";
}

export function staticAssetWorkerSource(): string {
  return `/* multica PWA service worker — matchers live in static-sw-policy.ts */
var STATIC_CACHE = ${JSON.stringify(STATIC_CACHE_NAME)};
var PAGE_CACHE = ${JSON.stringify(PAGE_CACHE_NAME)};
var LAST_DOCUMENT = ${JSON.stringify(LAST_DOCUMENT_PATH)};
var PRECACHE = ${JSON.stringify(PRECACHE_PATHS)};
var HASHED_STATIC_PATH = ${HASHED_STATIC_PATH};
var REVALIDATE_PATH = ${REVALIDATE_PATH};
var BYPASS_PATH = ${BYPASS_PATH};
var MARKETING_PATH = ${MARKETING_PATH};
var PWA_START_PATH = ${PWA_START_PATH};

function classify(request) {
  if (request.method !== "GET") return "ignore";
  var url;
  try {
    url = new URL(request.url);
  } catch (e) {
    return "ignore";
  }
  if (url.origin !== self.location.origin) return "ignore";
  if (BYPASS_PATH.test(url.pathname)) return "ignore";
  if (url.pathname === LAST_DOCUMENT) return "ignore";
  if (request.headers.get("RSC") === "1") return "ignore";
  if (request.headers.get("Next-Router-Prefetch") != null) return "ignore";
  if (url.searchParams.has("_rsc")) return "ignore";
  var isDocument =
    request.mode === "navigate" || request.destination === "document";
  if (isDocument) {
    if (url.pathname === "/" || MARKETING_PATH.test(url.pathname)) {
      return "ignore";
    }
    if (HASHED_STATIC_PATH.test(url.pathname)) return "ignore";
    return "network-first-document";
  }
  if (HASHED_STATIC_PATH.test(url.pathname)) return "cache-first";
  if (REVALIDATE_PATH.test(url.pathname)) return "stale-revalidate";
  return "ignore";
}

self.addEventListener("install", function (event) {
  event.waitUntil(
    caches
      .open(STATIC_CACHE)
      .then(function (cache) {
        return cache.addAll(PRECACHE).catch(function () {});
      })
      .then(function () {
        return self.skipWaiting();
      }),
  );
});

self.addEventListener("activate", function (event) {
  event.waitUntil(
    caches
      .keys()
      .then(function (keys) {
        return Promise.all(
          keys
            .filter(function (key) {
              var ours =
                key.indexOf("multica-static-") === 0 ||
                key.indexOf("multica-pages-") === 0;
              return ours && key !== STATIC_CACHE && key !== PAGE_CACHE;
            })
            .map(function (key) {
              return caches.delete(key);
            }),
        );
      })
      .then(function () {
        return self.clients.claim();
      }),
  );
});

self.addEventListener("fetch", function (event) {
  var action = classify(event.request);
  if (action === "ignore") return;
  if (action === "cache-first") {
    event.respondWith(cacheFirst(event.request));
    return;
  }
  if (action === "stale-revalidate") {
    event.respondWith(staleRevalidate(event.request));
    return;
  }
  if (action === "network-first-document") {
    event.respondWith(networkFirstDocument(event.request));
  }
});

function canStore(response) {
  return response.ok && response.type === "basic" && !response.redirected;
}

function cacheFirst(request) {
  return caches.open(STATIC_CACHE).then(function (cache) {
    return cache.match(request).then(function (cached) {
      if (cached) return cached;
      return fetch(request).then(function (response) {
        if (canStore(response)) cache.put(request, response.clone());
        return response;
      });
    });
  });
}

function staleRevalidate(request) {
  return caches.open(STATIC_CACHE).then(function (cache) {
    return cache.match(request).then(function (cached) {
      var fetching = fetch(request).then(function (response) {
        if (canStore(response)) cache.put(request, response.clone());
        return response;
      });
      return cached || fetching;
    });
  });
}

function storeDocument(cache, response) {
  if (!response.ok || response.type !== "basic") {
    return Promise.resolve();
  }
  // cache.put rejects a redirected Response. Rebuild from the body so a
  // /inbox → /{slug}/inbox launch still has a shell to reopen offline.
  return response.clone().blob().then(function (body) {
    var stored = new Response(body, {
      status: response.status,
      statusText: response.statusText,
      headers: response.headers,
    });
    cache.put(new Request(response.url), stored);
    cache.put(
      new Request(LAST_DOCUMENT),
      new Response(response.url, {
        headers: { "Content-Type": "text/plain" },
      }),
    );
  });
}

function restoreLastDocument(cache, request) {
  return cache.match(LAST_DOCUMENT).then(function (hint) {
    if (!hint) return Promise.reject(new TypeError("offline"));
    return hint.text().then(function (lastUrl) {
      var last;
      try {
        last = new URL(lastUrl);
      } catch (e) {
        return Promise.reject(new TypeError("offline"));
      }
      if (last.origin !== self.location.origin) {
        return Promise.reject(new TypeError("offline"));
      }
      var pathname = new URL(request.url).pathname;
      if (PWA_START_PATH.test(pathname) && last.href !== request.url) {
        return Response.redirect(last.href, 307);
      }
      return cache.match(last.href).then(function (page) {
        return page || Promise.reject(new TypeError("offline"));
      });
    });
  });
}

function networkFirstDocument(request) {
  return caches.open(PAGE_CACHE).then(function (cache) {
    return fetch(request)
      .then(function (response) {
        return storeDocument(cache, response).then(function () {
          return response;
        });
      })
      .catch(function () {
        return cache.match(request).then(function (cached) {
          if (cached) return cached;
          var url = new URL(request.url);
          if (url.search) {
            url.search = "";
            return cache.match(url.href).then(function (plain) {
              if (plain) return plain;
              return restoreLastDocument(cache, request);
            });
          }
          return restoreLastDocument(cache, request);
        });
      });
  });
}
`;
}
