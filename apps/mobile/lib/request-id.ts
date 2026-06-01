/**
 * Per-request ID. 8 random base36 chars + 4 base36 timestamp suffix —
 * collision-resistant enough for client-side telemetry, short enough to
 * eyeball in logs. Sent as `X-Request-ID` header so backend can correlate
 * its own log lines with the client.
 *
 * Mirrors the role of `createRequestId` in packages/core/utils.ts but
 * mobile owns its own implementation (zero core import).
 */
export function createRequestId(): string {
  const rand = Math.random().toString(36).slice(2, 10);
  const ts = Date.now().toString(36).slice(-4);
  return `${rand}${ts}`;
}
