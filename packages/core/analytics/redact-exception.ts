// PII scrubbing for `$exception` events before they leave the client.
//
// Exception autocapture (`capture_exceptions: true`) sends the error message
// and stack. Stack frames are code locations (file / line / function) and are
// safe, but a message often interpolates user input — a validation error with
// the typed value, a parse error with the raw text, a network error with a URL
// that may carry a token. We keep the diagnostic shape (type + frames + the
// non-sensitive part of the message) and redact the patterns that carry user
// data. Wired as posthog-js `before_send`; see initAnalytics.

const REDACTED = "[redacted]";

// Order matters: strip query strings before the generic long-token rule, so a
// URL's host isn't itself shredded.
const PATTERNS: Array<[RegExp, string]> = [
  // Emails.
  [/[a-z0-9._%+-]+@[a-z0-9.-]+\.[a-z]{2,}/gi, REDACTED],
  // URL query/fragment (may carry tokens / PII) — keep scheme+host+path.
  [/((?:https?|file|multica):\/\/[^\s?#]*)[?#]\S*/gi, `$1?${REDACTED}`],
  // Long opaque tokens: JWTs, API keys, UUIDs, session ids (24+ chars).
  [/\b[A-Za-z0-9_-]{24,}\b/g, REDACTED],
];

/** Redact PII-ish substrings from a free-text string. */
export function redactText(input: unknown): unknown {
  if (typeof input !== "string" || input.length === 0) return input;
  let out = input;
  for (const [pattern, replacement] of PATTERNS) {
    out = out.replace(pattern, replacement);
  }
  return out;
}

/**
 * Redact the user-facing strings on a `$exception` event's properties in
 * place: the top-level message and every entry's `value` in `$exception_list`.
 * Types and stack frames are left untouched (code locations, not user data).
 * Returns the same properties object for chaining.
 */
export function redactExceptionProperties(
  properties: Record<string, unknown> | undefined,
): Record<string, unknown> | undefined {
  if (!properties || typeof properties !== "object") return properties;

  if ("$exception_message" in properties) {
    properties.$exception_message = redactText(properties.$exception_message);
  }

  const list = properties.$exception_list;
  if (Array.isArray(list)) {
    for (const entry of list) {
      if (entry && typeof entry === "object" && "value" in entry) {
        (entry as { value: unknown }).value = redactText(
          (entry as { value: unknown }).value,
        );
      }
    }
  }

  return properties;
}
