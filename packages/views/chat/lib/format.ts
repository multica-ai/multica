/**
 * Format an elapsed seconds value as `Ns` (under a minute) or `Nm Ms`
 * (over a minute). Drops the seconds part when the remainder is 0 to
 * keep round-minute readings short ("3m" rather than "3m 0s"). Shared
 * by the live StatusPill timer and the persistent assistant-message
 * timing line — keeping them in lockstep avoids visible drift between
 * "Working · 38s" mid-flight and a final "Replied in 39s" caption.
 */
export function formatElapsedSecs(secs: number): string {
  if (secs < 60) return `${secs}s`;
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s ? `${m}m ${s}s` : `${m}m`;
}

/** Convenience: same formatting, but the input is milliseconds (server-stored elapsed_ms). */
export function formatElapsedMs(ms: number): string {
  return formatElapsedSecs(Math.max(0, Math.round(ms / 1000)));
}

/**
 * Wall-clock time for the reply caption: today → clock time, an earlier day
 * → numeric date + clock time, with the year added when it differs from
 * `now`. Every tier keeps the time of day — the caption answers "when did
 * this reply land", which a bare date cannot. Returns "" for a missing or
 * unparseable timestamp so captions can drop the time part silently. `now`
 * is injectable for deterministic tests.
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
