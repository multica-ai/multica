/**
 * Error types for the push notification flow.
 *
 * The settings UI distinguishes between user-recoverable problems
 * (browser blocked, permission denied) and infrastructure failures
 * (VAPID not configured, rate limit hit). Surfacing them as typed
 * exceptions keeps the UI logic simple.
 */

export type PushSubscriptionErrorReason =
  /** Feature not available in this browser (e.g. Safari, in-app webviews). */
  | "unsupported"
  /** User denied the OS-level notification permission prompt. */
  | "permission_denied"
  /** The server has no VAPID public key configured. */
  | "vapid_not_configured"
  /** Server rejected the request (4xx, 5xx). */
  | "server_error"
  /** Catch-all for unexpected client-side errors. */
  | "unknown";

export class PushSubscriptionError extends Error {
  readonly reason: PushSubscriptionErrorReason;

  constructor(reason: PushSubscriptionErrorReason, message?: string) {
    super(message ?? reason);
    this.name = "PushSubscriptionError";
    this.reason = reason;
  }
}
