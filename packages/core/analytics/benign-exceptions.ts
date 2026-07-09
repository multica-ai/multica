// Known-benign browser exceptions that are pure noise in `$exception`
// telemetry. These are dropped ENTIRELY in `before_send` (not merely deduped by
// exception-dedupe.ts) — they carry no actionable signal, the browser
// self-recovers, and at scale they dominate the error stream, drowning real
// failures and burning the billed event budget.
//
// ResizeObserver "loop ..." errors are the canonical case: the spec fires them
// when observation callbacks don't settle within a single animation frame. The
// browser resumes delivery on the next frame, so nothing actually breaks. Every
// app that uses ResizeObserver (directly or via a UI library) emits them. The
// CSSWG explicitly considers them benign — see w3c/csswg-drafts#5023. Across
// Chrome versions the message is either "ResizeObserver loop limit exceeded"
// (older) or "ResizeObserver loop completed with undelivered notifications"
// (newer); both contain "ResizeObserver loop".
//
// The bar for adding a pattern here is high: it must be a benign,
// self-recovering error with no actionable signal. A real bug must never be
// silenced — when unsure, leave it to the dedupe fuse, which only caps repeats.

const BENIGN_MESSAGE_PATTERNS: RegExp[] = [/ResizeObserver loop/i];

/**
 * Whether this `$exception` event is known-benign browser noise that should be
 * dropped entirely. Reads the message from the (pre-redaction) event
 * properties — the matched messages carry no PII, so reading them raw is safe,
 * and matching before redaction avoids any chance of a scrub mangling the
 * signal. Never throws: any unexpected shape returns `false` (keep the event),
 * the fail-open direction `before_send` requires.
 */
export function isBenignException(
  properties: Record<string, unknown> | undefined,
): boolean {
  if (!properties || typeof properties !== "object") return false;

  const messages: unknown[] = [properties.$exception_message];
  const list = properties.$exception_list;
  if (Array.isArray(list)) {
    for (const entry of list) {
      if (entry && typeof entry === "object" && "value" in entry) {
        messages.push((entry as { value: unknown }).value);
      }
    }
  }

  for (const message of messages) {
    if (typeof message !== "string") continue;
    for (const pattern of BENIGN_MESSAGE_PATTERNS) {
      if (pattern.test(message)) return true;
    }
  }
  return false;
}
