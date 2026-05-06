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

/**
 * Resolve the badge count for an SW push event. Trusts the count the server
 * stamped on the payload when present; falls back to /api/me/inbox/unread-count
 * when the field is missing or malformed (no-data wake-up push, JSON parse
 * failure, schema drift). Returns undefined only if both sources fail.
 *
 * iOS WebKit auto-increments the PWA badge by 1 whenever the SW shows a
 * notification but does not call setAppBadge during the push event. The
 * fallback fetch ensures the SW always has a count to apply, so we don't
 * silently leak the badge upward push by push.
 */
export async function resolveBadgeCount(
  payloadCount: number | undefined,
  fetchImpl: typeof fetch = fetch,
): Promise<number | undefined> {
  if (typeof payloadCount === "number" && Number.isFinite(payloadCount)) {
    return payloadCount;
  }
  try {
    const res = await fetchImpl("/api/me/inbox/unread-count", {
      credentials: "same-origin",
    });
    if (!res.ok) return undefined;
    const data = (await res.json()) as { count?: unknown };
    if (typeof data.count === "number" && Number.isFinite(data.count)) {
      return data.count;
    }
    return undefined;
  } catch {
    return undefined;
  }
}
