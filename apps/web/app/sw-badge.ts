// Pure badge logic shared by the service worker (push handler) and the
// page-side bridge. Takes a navigator-like object as a parameter so it can
// be reused under WebWorker (`self.navigator`) and DOM (`window.navigator`)
// without leaking either lib into the other.

export type BadgingNavigator = {
  setAppBadge?: (count?: number) => Promise<void>;
  clearAppBadge?: () => Promise<void>;
};

/**
 * Set or clear the OS app-icon badge from a count.
 *
 * - Negative / zero / NaN count → clearAppBadge (treats undefined as a no-op)
 * - Positive count → setAppBadge(count)
 * - Browser without Badging API (older Safari, Firefox) → silent no-op
 *
 * Returns the promise the browser would have surfaced from the underlying
 * call so callers can `event.waitUntil(...)` it inside a service worker.
 */
export function applyAppBadge(
  nav: BadgingNavigator | undefined,
  count: number | undefined,
): Promise<void> | undefined {
  if (typeof count !== "number" || !Number.isFinite(count)) return undefined;
  if (!nav) return undefined;
  if (count <= 0) {
    return nav.clearAppBadge?.().catch(() => undefined);
  }
  return nav.setAppBadge?.(count).catch(() => undefined);
}
