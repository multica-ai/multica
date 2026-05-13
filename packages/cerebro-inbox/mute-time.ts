/**
 * Compute the timestamp the inbox-mute action should mute until — the next
 * occurrence of 08:00 in the user's local timezone.
 *
 * If we are at exactly 08:00, the next occurrence is tomorrow's 08:00.
 */
export function nextLocalEightAm(now: Date = new Date()): Date {
  const target = new Date(now);
  target.setHours(8, 0, 0, 0);
  if (target.getTime() <= now.getTime()) {
    target.setDate(target.getDate() + 1);
  }
  return target;
}

/** Whether an inbox item is currently muted (muted_until is in the future). */
export function isMuted(mutedUntil: string | null | undefined, now: Date = new Date()): boolean {
  if (!mutedUntil) return false;
  return new Date(mutedUntil).getTime() > now.getTime();
}

/**
 * Format a `muted_until` ISO timestamp as a short HH:MM-style time string
 * in the user's locale. We deliberately use `Intl.DateTimeFormat` rather
 * than `toLocaleTimeString({ hour, minute })` so locales that prefer 12-hour
 * clocks still get a clean "8:00 AM" rendering.
 *
 * Returns `null` for missing/expired values so callers can fall back to the
 * row's regular timestamp.
 */
export function formatMutedUntilTime(
  mutedUntil: string | null | undefined,
  locale?: string,
  now: Date = new Date(),
): string | null {
  if (!isMuted(mutedUntil, now)) return null;
  const fmt = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit" });
  return fmt.format(new Date(mutedUntil!));
}
