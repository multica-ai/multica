/* global self */
// Multica service worker — minimal, install-enabling only.
//
// Scope: this file deliberately does NOT cache anything. Its sole purpose is to
// satisfy the "installable PWA" criteria so browsers expose the address-bar
// install affordance and the app launches in a standalone window.
//
// Why a no-op `fetch` listener exists: historically Chromium required a service
// worker with a fetch handler before offering installation. That requirement
// has since been relaxed in favor of a manifest + secure origin, but registering
// a controlling worker with a fetch handler remains the most reliable way to get
// the install prompt across browsers and versions. The handler intentionally
// does nothing — every request falls through to the network untouched, so there
// is no caching of auth-scoped or multi-tenant data to reason about.
//
// Offline support and asset caching are explicit non-goals here; they can be
// layered on later by giving the fetch handler a real strategy. NOTE: doing so
// must revisit the update lifecycle below — with real caching, the immediate
// skipWaiting() + clients.claim() would swap the active worker (and its cache
// policy) under already-open tabs, so a versioning/activation strategy would be
// required at that point.

self.addEventListener("install", () => {
  // Activate this worker immediately instead of waiting for existing tabs to
  // close, so installability is satisfied on first load.
  self.skipWaiting();
});

self.addEventListener("activate", (event) => {
  // Take control of already-open clients so the worker is "controlling" right
  // away — another signal browsers use when deciding installability.
  event.waitUntil(self.clients.claim());
});

self.addEventListener("fetch", () => {
  // Intentionally empty: do not call respondWith, so the request proceeds to
  // the network as if no service worker were present. Present only to enable
  // installability.
});
