/**
 * Retry policy for inbox reads (MUL-9635).
 *
 * The server bounds inbox reads and answers `503` with `Retry-After` when it
 * is busy (docs/inbox-read-protection.md). One busy answer must not leave the
 * Inbox looking empty until the next refresh, so a transient failure is tried
 * again a few times — never sooner than the server asked, backing off and
 * spreading clients apart so a busy server is not hit in lockstep. Any other
 * failure keeps the app-wide single retry.
 */

/** Statuses that say "try again shortly", not "this request is wrong". */
const TRANSIENT_STATUSES = new Set([408, 429, 502, 503, 504]);

/** Retries after a transient failure, so at most four requests per read. */
export const INBOX_TRANSIENT_RETRIES = 3;
/** Retries after any other failure: the app-wide default. */
const OTHER_RETRIES = 1;

const BASE_DELAY_MS = 1000;
const MAX_BACKOFF_MS = 8000;
const JITTER = 0.25;

function errorStatus(error: unknown): number | undefined {
  const status = (error as { status?: unknown } | null)?.status;
  return typeof status === "number" ? status : undefined;
}

/**
 * Whether a failed inbox read is worth trying again shortly: the server said
 * it is busy or unreachable, or the request never got an answer. Read off the
 * error's shape, not its class, so mocked or differently built clients count.
 */
export function isTransientInboxError(error: unknown): boolean {
  const status = errorStatus(error);
  if (status !== undefined) return TRANSIENT_STATUSES.has(status);
  // `fetch` rejects with a TypeError when the request could not be sent.
  return error instanceof TypeError;
}

/** TanStack `retry`: `failureCount` is how many attempts have already been retried. */
export function inboxQueryRetry(failureCount: number, error: unknown): boolean {
  return failureCount < (isTransientInboxError(error) ? INBOX_TRANSIENT_RETRIES : OTHER_RETRIES);
}

/**
 * TanStack `retryDelay`: exponential backoff from one second, never earlier
 * than the server's `Retry-After`, plus up to a quarter more so clients that
 * failed together do not come back together.
 */
export function inboxQueryRetryDelay(failureCount: number, error: unknown, random: () => number = Math.random): number {
  const backoff = Math.min(BASE_DELAY_MS * 2 ** failureCount, MAX_BACKOFF_MS);
  const retryAfter = (error as { retryAfterMs?: unknown } | null)?.retryAfterMs;
  const floor = typeof retryAfter === "number" && retryAfter > 0 ? retryAfter : 0;
  const delay = Math.max(backoff, floor);
  return Math.round(delay * (1 + JITTER * random()));
}
