/**
 * Map backend auth errors to user-facing strings. The backend returns raw
 * English messages that are fine for logs but should not surface as-is —
 * we map the known shapes to friendlier copy and fall back to the caller's
 * default for anything unrecognised.
 */
export function mapAuthError(err: unknown, fallback: string): string {
  if (!(err instanceof Error)) return fallback;
  const msg = err.message.toLowerCase();
  if (/invalid|incorrect|wrong/.test(msg)) {
    return "That code didn't match. Double-check and try again.";
  }
  if (/expired/.test(msg)) {
    return "That code has expired. Tap resend to get a new one.";
  }
  if (/rate.?limit|too many|throttle/.test(msg)) {
    return "Too many attempts. Wait a moment and try again.";
  }
  if (/network|fetch|timeout|unreachable/.test(msg)) {
    return "Can't reach Multica. Check your connection and retry.";
  }
  return fallback;
}
