/**
 * Sliding session renewal (MUL-7436).
 *
 * Mirrors packages/core/platform/session-renewal.ts in contract, not in code:
 * mobile owns its own API client and stores the token in the Keychain behind
 * an async API, so this is a mobile-local implementation of the same two
 * rules.
 *
 * Renewal follows USE, not time. There is no interval timer — a check runs at
 * launch and whenever the app returns to the foreground. A backgrounded phone
 * is the normal state of a phone, and a timer there would keep a session
 * alive for a user who stopped using the app months ago.
 *
 * Renewal is not a session event. It swaps a credential in place and must
 * never look like a login or a logout. Offline, 5xx and a malformed response
 * all leave the current session untouched — but leaving it alone is not the
 * same as waiting: a failure happens INSIDE the renewal window, where the
 * remaining lifetime is under half a TTL and shrinking, so the retry has to
 * come back quickly instead of consuming the normal cadence. Only a real 401
 * ends a session, and `onUnauthorized` in app/_layout.tsx already owns that.
 */
import { api, ApiError } from "./api";
import { commitRenewedToken, getToken } from "./secure-storage";
import { currentSessionEpoch, sessionEpochChanged } from "./session-epoch";

/**
 * How long to wait after a FAILED check before another is allowed, by
 * consecutive failure — deliberately not the normal cadence.
 *
 * The normal cadence comes from the token's lifetime and can be days. A
 * failure that consumed it would be fatal in the ordinary case, not only at
 * short TTLs: a client told "check again in 3 days" on day 12, returning on
 * day 29 with one day left and hitting a single 503, would not be allowed to
 * retry until day 32 — two days after the session expired. A failure buys a
 * short backoff instead, reset by the next success. Mirrors
 * RETRY_DELAYS_MS in packages/core/platform/session-renewal.ts.
 */
const RETRY_DELAYS_MS = [1_000, 2_000, 4_000, 8_000, 16_000, 30_000] as const;

/**
 * Anti-busy-loop floor, not a policy. It matches the server's own floor, so it
 * never overrides a cadence the server actually chose — a floor above the
 * server's smallest value would silently let an active session expire. See
 * minSessionRenewCheckInterval in server/internal/auth/session.go.
 */
const MIN_CHECK_INTERVAL_MS = 5 * 1000;

// The earliest a check may run. Zero at start, so the launch check fires on
// the first trigger; a success pushes it out by the server's cadence and a
// failure by a short backoff. Tracking the deadline rather than the last
// attempt is what keeps those two from sharing one number.
let nextCheckAt = 0;
let consecutiveFailures = 0;

/** Push the next allowed check out by the current backoff step. */
function scheduleRetryAfterFailure(): void {
  const delay =
    RETRY_DELAYS_MS[Math.min(consecutiveFailures, RETRY_DELAYS_MS.length - 1)]!;
  consecutiveFailures += 1;
  nextCheckAt = Date.now() + delay;
}
// Collapses concurrent triggers: launch and a foreground transition can land
// together, and two renewals would leave two valid tokens racing to be the
// one written to the Keychain.
let inFlight: Promise<void> | null = null;

/**
 * Run a renewal check now, regardless of how recently one ran. Use at launch;
 * everywhere else prefer `maybeRenewSession`.
 */
export async function renewSessionNow(): Promise<void> {
  if (inFlight) return inFlight;

  // Captured before anything awaits. Comparing tokens alone is not enough on
  // mobile: logout's Keychain delete is async, so a read taken after logout
  // began but before the delete landed still returns the old token, and this
  // attempt would then write its replacement back over a session the user
  // just ended. The epoch moves synchronously at the start of logout, a 401
  // teardown and a new sign-in, so it is already stale by the time we check.
  const epochAtStart = currentSessionEpoch();

  // Assigned before the first await, so two callers in the same tick cannot
  // both get past the guard above. Reading the Keychain is itself async, so
  // doing that first — outside the promise — would reopen the window this
  // guard exists to close.
  inFlight = (async () => {
    try {
      // The session this attempt is for. Anything applied below is checked
      // against it, so an attempt that outlives its own session writes
      // nothing.
      const startedFrom = await getToken();
      if (!startedFrom || sessionEpochChanged(epochAtStart)) return;

      const result = await api.refreshSession();
      // A response that carries no cadence is not a usable answer. The schema
      // fallback produces exactly this shape — `parseWithFallback` returns
      // zeroes for a body that failed validation instead of throwing — and a
      // real server always sends a positive interval. Left untreated it would
      // reset the failure count without moving the deadline, so every
      // subsequent interaction fires another request. Back off as for a
      // failure.
      if (result.check_again_in_seconds <= 0) {
        scheduleRetryAfterFailure();
      } else {
        consecutiveFailures = 0;
        nextCheckAt =
          Date.now() +
          Math.max(MIN_CHECK_INTERVAL_MS, result.check_again_in_seconds * 1000);
      }
      // A token that did arrive is still worth applying, whatever the cadence
      // field said.
      if (!result.renewed || !result.token) return;

      // The decision to keep this result and the write that acts on it happen
      // together, inside the credential writer's serialization — so a logout
      // or a sign-in cannot slip between them. A refusal is just a no-op:
      // nothing is deleted, because by the time a renewal is stale whatever is
      // stored may belong to a session that started after it.
      const applied = await commitRenewedToken(result.token, {
        epoch: epochAtStart,
        previousToken: startedFrom,
      });
      if (!applied) return;

      // The write is committed; publishing it in memory is the last step. A
      // teardown that lands in this gap has already cleared storage and will
      // clear the in-memory token too, so stay out of its way.
      if (sessionEpochChanged(epochAtStart)) return;
      api.setToken(result.token);
    } catch (err) {
      // A 401 has already been routed to the sign-out path by the client's
      // onUnauthorized hook. Everything else — airplane mode, a captive
      // portal, a 5xx — says nothing about the session, which just
      // authenticated this request. Keep it, and come back soon: the window
      // this failed inside is still closing.
      scheduleRetryAfterFailure();
      if (!(err instanceof ApiError) || err.status !== 401) {
        console.log("[auth] session renewal deferred", err);
      }
    } finally {
      inFlight = null;
    }
  })();

  return inFlight;
}

/** Run a check unless one is not due yet. */
export function maybeRenewSession(): void {
  if (Date.now() < nextCheckAt) return;
  void renewSessionNow();
}

/** Test seam: forget the cadence and the last-attempt timestamp. */
export function resetSessionRenewalForTest(): void {
  nextCheckAt = 0;
  consecutiveFailures = 0;
  inFlight = null;
}
