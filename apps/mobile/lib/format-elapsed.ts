/**
 * Format elapsed durations for chat timing captions.
 *
 * Mirrors `packages/views/chat/lib/format.ts` so the live StatusPill timer
 * (`Thinking · 38s`) and the persistent post-reply caption (`Replied in 39s`)
 * read identically across web / desktop / mobile.
 */
import { translate } from "@/i18n";

export function formatElapsedSecs(secs: number): string {
  if (secs < 60) return translate("{{count}}s", { count: secs });
  const m = Math.floor(secs / 60);
  const s = secs % 60;
  return s
    ? translate("{{minutes}}m {{seconds}}s", { minutes: m, seconds: s })
    : translate("{{count}}m", { count: m });
}

/** Same formatting, but the input is milliseconds (server-stored `elapsed_ms`). */
export function formatElapsedMs(ms: number): string {
  return formatElapsedSecs(Math.max(0, Math.round(ms / 1000)));
}
