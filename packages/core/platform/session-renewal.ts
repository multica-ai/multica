import type { ApiClient } from "../api/client";
import type { StorageAdapter } from "../types/storage";
import type { Logger } from "../logger";

/**
 * Sliding session renewal for clients that hold the session as a string
 * (Desktop; web only while a legacy localStorage token is still in play).
 * Cookie-mode browsers are renewed by the server inline on any authenticated
 * request and never come through here — see middleware.Auth (MUL-7436).
 *
 * Two properties shape everything below.
 *
 * Renewal follows USE, not time. There is no interval timer: a check runs at
 * launch, when the app comes back to the foreground, and when the user
 * actually interacts with it. A timer would keep an abandoned but still-open
 * window's session alive indefinitely, which is the one thing a session
 * lifetime is for.
 *
 * Renewal is not a session event. It replaces a credential in place; it must
 * never look like a login or a logout to anything downstream. A failed
 * renewal is a no-op — the session it was trying to extend is still valid for
 * at least another half TTL, so offline, 5xx and a malformed response all
 * leave everything exactly as it was. Only the server actually rejecting the
 * credential ends a session, and that path already exists (ApiClient's 401
 * handling).
 */

const TOKEN_STORAGE_KEY = "multica_token";

/**
 * Governs one case only: the launch check failed and has to be retried before
 * any server cadence is known — every answered check replaces it. Thirty
 * seconds fits inside the renewal window of even the shortest supported
 * AUTH_TOKEN_TTL (one minute, window 30s), so it cannot be the reason an
 * actively used session expires, and it is long enough that an offline app is
 * not retrying in a loop.
 */
const FALLBACK_CHECK_INTERVAL_MS = 30 * 1000;

/**
 * Anti-busy-loop floor, not a policy. The server owns the cadence and
 * guarantees its value fits inside the renewal window; this only stops a
 * nonsense `check_again_in_seconds` from turning activity into a request per
 * event. It matches the server's own floor, so it never overrides a value the
 * server actually chose — a floor above that would silently let an active
 * session expire. See minSessionRenewCheckInterval in
 * server/internal/auth/session.go.
 */
const MIN_CHECK_INTERVAL_MS = 5 * 1000;

export interface SessionRenewalOptions {
  api: ApiClient;
  storage: StorageAdapter;
  /**
   * Whether a session is live right now. Renewal is skipped while logged out
   * or still booting — there is nothing to extend, and a request fired during
   * the identity probe would race it.
   */
  isAuthenticated: () => boolean;
  logger?: Logger;
}

export interface SessionRenewal {
  /**
   * Run a check unless one ran recently. Safe to call on every user
   * interaction — the interval check makes all but the first a no-op.
   */
  maybeRenew: () => void;
  /** Run a check now, ignoring the interval. Used at app start. */
  renewNow: () => Promise<void>;
  /** Test seam: forget the last-attempt timestamp and cadence. */
  reset: () => void;
}

export function createSessionRenewal({
  api,
  storage,
  isAuthenticated,
  logger,
}: SessionRenewalOptions): SessionRenewal {
  let checkIntervalMs = FALLBACK_CHECK_INTERVAL_MS;
  let lastAttemptAt = 0;
  // Collapses concurrent triggers — a foreground event and a click in the
  // same moment must not produce two renewals, which would leave two valid
  // tokens racing to be the one written to storage.
  let inFlight: Promise<void> | null = null;

  async function renewNow(): Promise<void> {
    if (inFlight) return inFlight;
    if (!isAuthenticated()) return;

    // The session this attempt is FOR. Everything applied at the end is
    // checked against it: an attempt that outlives its own session must not
    // write anything back.
    const startedFrom = storage.getItem(TOKEN_STORAGE_KEY);
    if (!startedFrom) return;

    lastAttemptAt = Date.now();

    inFlight = api
      .refreshSession()
      .then((result) => {
        if (result.check_again_in_seconds > 0) {
          checkIntervalMs = Math.max(
            MIN_CHECK_INTERVAL_MS,
            result.check_again_in_seconds * 1000,
          );
        }
        if (!result.renewed || !result.token) return;

        // Late-result guard. Between the request and this line the user may
        // have logged out, switched accounts, or had the session expire — all
        // of which replace or clear the stored token. Writing our token back
        // over any of those would resurrect a session the app has already
        // torn down.
        if (!isAuthenticated()) return;
        if (storage.getItem(TOKEN_STORAGE_KEY) !== startedFrom) return;

        // Order matters: persist before the in-memory client, so a crash
        // between the two lines leaves the newer credential on disk rather
        // than an older one that requests have already stopped using.
        storage.setItem(TOKEN_STORAGE_KEY, result.token);
        api.setToken(result.token);
        logger?.info("session renewed", { expiresAt: result.expires_at });
      })
      .catch((err: unknown) => {
        // Offline, 5xx, a server-side signing failure, a response that failed
        // its schema — none of these say anything about whether the session
        // is still good, and it demonstrably is: it authenticated this very
        // request path moments ago. Keep it and try again later.
        //
        // A genuine 401 is not handled here either, but for the opposite
        // reason: ApiClient already routed it to the session-expiry path
        // before this catch ran.
        logger?.debug("session renewal did not complete; keeping the current session", {
          error: err instanceof Error ? err.message : String(err),
        });
      })
      .finally(() => {
        inFlight = null;
      });

    return inFlight;
  }

  function maybeRenew(): void {
    if (Date.now() - lastAttemptAt < checkIntervalMs) return;
    void renewNow();
  }

  return {
    maybeRenew,
    renewNow,
    reset: () => {
      lastAttemptAt = 0;
      checkIntervalMs = FALLBACK_CHECK_INTERVAL_MS;
      inFlight = null;
    },
  };
}

/**
 * Wire a renewal controller to the signals that mean "someone is using this
 * app": returning to the foreground, and interacting with it. Returns a
 * teardown.
 *
 * Deliberately no interval timer. `maybeRenew` is cheap when it declines, and
 * an app nobody is touching should not be renewing anything.
 */
export function watchSessionActivity(renewal: SessionRenewal): () => void {
  if (typeof window === "undefined" || typeof document === "undefined") {
    return () => {};
  }

  const onVisible = () => {
    if (document.visibilityState === "visible") renewal.maybeRenew();
  };
  const onActivity = () => renewal.maybeRenew();

  document.addEventListener("visibilitychange", onVisible);
  window.addEventListener("focus", onActivity);
  // Passive so the listeners never delay scrolling or input handling; they do
  // nothing but read a timestamp on all but one call in every check interval.
  window.addEventListener("pointerdown", onActivity, { passive: true });
  window.addEventListener("keydown", onActivity, { passive: true });

  return () => {
    document.removeEventListener("visibilitychange", onVisible);
    window.removeEventListener("focus", onActivity);
    window.removeEventListener("pointerdown", onActivity);
    window.removeEventListener("keydown", onActivity);
  };
}
