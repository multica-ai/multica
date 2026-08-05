let registrationPromise: Promise<ServiceWorkerRegistration> | null = null;

/**
 * Registers `/sw.js` and returns the registration handle, so later features
 * (pushManager subscriptions, registration.showNotification) can build on it
 * without changing how registration works. Call sites that only need the
 * side effect can ignore the return value.
 *
 * Idempotent per page load. Returns null where service workers are
 * unavailable (SSR, unsupported or insecure contexts).
 */
export function registerServiceWorker(): Promise<ServiceWorkerRegistration> | null {
  if (typeof navigator === "undefined" || !("serviceWorker" in navigator)) {
    return null;
  }
  if (registrationPromise) return registrationPromise;

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

  registrationPromise = navigator.serviceWorker.register("/sw.js");
  return registrationPromise;
}
