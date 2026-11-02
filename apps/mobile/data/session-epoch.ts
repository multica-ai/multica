/**
 * A synchronous marker for "the credential this app is holding has changed".
 *
 * Mobile stores the token in the Keychain, and every Keychain operation is
 * async. That makes "is the session I started from still the live one?"
 * unanswerable by reading storage: logout does `await clearToken()`, so
 * between the moment logout begins and the moment the delete lands, a read
 * still returns the old token. A background session renewal that sampled
 * storage in that window would see a token that is on its way out, then write
 * its replacement back — resurrecting a session the user just ended, on the
 * device they ended it from.
 *
 * The epoch closes that window because incrementing it is synchronous and
 * happens FIRST, before any await in the path that invalidates the session.
 * Anything long-running captures the epoch before its first await and refuses
 * to apply a result if the value moved.
 *
 * Deliberately module state and not a store: it has to be readable and
 * writable without React, without awaiting, and from both the auth store and
 * the API client's 401 handler.
 */
let epoch = 0;

/** Read the current epoch. Capture this BEFORE the first await. */
export function currentSessionEpoch(): number {
  return epoch;
}

/**
 * Mark every credential captured so far as stale. Call synchronously at the
 * START of anything that ends or replaces the session — logout, a 401
 * teardown, signing in as someone else — before awaiting the storage writes
 * that carry it out.
 */
export function invalidateSessionEpoch(): void {
  epoch++;
}

/** True when the session has been invalidated or replaced since `at`. */
export function sessionEpochChanged(at: number): boolean {
  return epoch !== at;
}
