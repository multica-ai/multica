/**
 * Format elapsed durations for chat timing captions.
 *
 * Mirrors `packages/views/chat/lib/format.ts` so the live StatusPill timer
 * (`Thinking · 38s`) and the persistent post-reply caption (`Replied in 39s`)
 * read identically across web / desktop / mobile.
 */
export function formatElapsedSecs(secs: number): string {
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

/** Same formatting, but the input is milliseconds (server-stored `elapsed_ms`). */
export function formatElapsedMs(ms: number): string {
  return formatElapsedSecs(Math.max(0, Math.round(ms / 1000)));
}
