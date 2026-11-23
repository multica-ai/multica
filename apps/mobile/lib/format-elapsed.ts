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

/**
 * Wall-clock time for the reply caption: today → clock time, an earlier day
 * → numeric date + clock time, with the year added when it differs from
 * `now`. Mirrors `formatMessageTime` in packages/views/chat/lib/format.ts so
 * the caption reads identically across web / desktop / mobile. Returns ""
 * for a missing or unparseable timestamp. `now` is injectable for
 * deterministic tests.
 */
export function formatMessageTime(
  dateStr: string,
  locale: string,
  now: Date = new Date(),
): string {
  const date = new Date(dateStr);
  if (Number.isNaN(date.getTime())) return "";
  if (date.toDateString() === now.toDateString()) {
    return date.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit" });
  }
  return date.toLocaleString(locale, {
    ...(date.getFullYear() !== now.getFullYear() ? { year: "numeric" } : {}),
    month: "numeric",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  });
}
