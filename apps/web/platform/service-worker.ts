let serviceWorkerPromise: Promise<ServiceWorkerRegistration | null> | null = null;

async function removeDevelopmentServiceWorker(): Promise<null> {
  const registrations = await navigator.serviceWorker.getRegistrations();
  await Promise.all(registrations.map((registration) => registration.unregister()));

  if ("caches" in globalThis) {
    const cacheNames = await caches.keys();
    await Promise.all(cacheNames.map((cacheName) => caches.delete(cacheName)));
  }

  return null;
}

/**
 * Registers `/sw.js` and returns the registration handle, so later features
 * (pushManager subscriptions, registration.showNotification) can build on it
 * without changing how registration works. Call sites that only need the
 * side effect can ignore the return value.
 *
 * In non-production builds, removes any existing registrations and caches so
 * a worker previously installed on localhost cannot keep controlling dev
 * navigations. Idempotent per page load. Returns null where service workers
 * are unavailable (SSR, unsupported or insecure contexts).
 */
export function registerServiceWorker():
  | Promise<ServiceWorkerRegistration | null>
  | null {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) {
    return null;
  }
  if (serviceWorkerPromise) return serviceWorkerPromise;

  if (process.env.NODE_ENV !== "production") {
    serviceWorkerPromise = removeDevelopmentServiceWorker();
    return serviceWorkerPromise;
  }

  // Auto-reload when a new worker takes over, so every open tab runs the
  // current worker. Skipped for the very first claim: reloading there would
  // interrupt the page right after login for no behavioural difference.
  let hadController = navigator.serviceWorker.controller !== null;
  navigator.serviceWorker.addEventListener("controllerchange", () => {
    if (!hadController) {
      hadController = true;
      return;
    }
    window.location.reload();
  });

  serviceWorkerPromise = navigator.serviceWorker.register("/sw.js");
  return serviceWorkerPromise;
}
