/**
 * Pure classification for the daemon auth probe. Kept free of Electron imports
 * so it can be unit-tested in jsdom.
 *
 * When the local daemon fails to reach "running" shortly after a start, the
 * main process probes the daemon's token against the backend (GET /api/me) to
 * tell "the daemon can't authenticate" apart from "the daemon is slow / the
 * network is down / it crashed for another reason". Misclassifying a network
 * blip as an auth failure would be worse than the original silent-Starting bug,
 * so the rules below are deliberately conservative: only an explicit 401 (or a
 * missing credential) is treated as auth-expired.
 */

export interface AuthProbeOutcome {
  /** HTTP status code returned by the probe request, if one completed. */
  status?: number;
  /** The daemon profile has no token at all — there is nothing to validate. */
  noToken?: boolean;
  /** The probe request threw (timeout, connection refused, DNS, TLS). */
  networkError?: boolean;
}

export type AuthProbeResult = "auth_expired" | "ok" | "unknown";

/**
 * Whether an error represents a genuine auth rejection (HTTP 401) as opposed to
 * a transient failure (5xx, network, local I/O). Used by the re-authenticate
 * flow so that only a real 401 — the session token itself is dead — forces a
 * full re-login; transient failures keep the user signed in to retry.
 *
 * `mintPat` attaches the response status to the error it throws, so a 401
 * surfaces here as `{ status: 401 }`. Everything else (no status, 5xx, a thrown
 * fetch, a file-write error) is treated as non-auth.
 */
export function isAuthStatusError(err: unknown): boolean {
  return (
    typeof err === "object" &&
    err !== null &&
    (err as { status?: unknown }).status === 401
  );
}

export function classifyAuthProbe(outcome: AuthProbeOutcome): AuthProbeResult {
  // No credential to validate → the user must sign in.
  if (outcome.noToken) return "auth_expired";
  // Couldn't reach the server → this is a network problem, not an auth one.
  // Stay "unknown" so the caller keeps showing "starting"/"stopped" instead of
  // wrongly prompting for re-login.
  if (outcome.networkError) return "unknown";
  // The server explicitly rejected the token.
  if (outcome.status === 401) return "auth_expired";
  // The token is accepted — the daemon is failing for some other reason.
  if (outcome.status !== undefined && outcome.status >= 200 && outcome.status < 300) {
    return "ok";
  }
  // 5xx and everything else are inconclusive about the token's validity.
  return "unknown";
}
