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

export function addHours(hours: number, now: Date = new Date()): Date {
  return new Date(now.getTime() + hours * 60 * 60 * 1000);
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

export function formatPlannedDateTime(
  value: string | null | undefined,
  locale?: string,
): string | null {
  if (!value) return null;
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(new Date(value));
}

export function nextLocalNineAm(now: Date = new Date()): Date {
  const target = new Date(now);
  target.setDate(target.getDate() + 1);
  target.setHours(9, 0, 0, 0);
  return target;
}

export function nextBusinessDayNineAm(now: Date = new Date()): Date {
  const target = new Date(now);
  target.setDate(target.getDate() + 1);
  while (target.getDay() === 0 || target.getDay() === 6) {
    target.setDate(target.getDate() + 1);
  }
  target.setHours(9, 0, 0, 0);
  return target;
}

export function nextMondayNineAm(now: Date = new Date()): Date {
  const target = new Date(now);
  const daysUntilMonday = (8 - target.getDay()) % 7 || 7;
  target.setDate(target.getDate() + daysUntilMonday);
  target.setHours(9, 0, 0, 0);
  return target;
}

export function toDateTimeLocalValue(date: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}T${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

/**
 * Inverse of {@link toDateTimeLocalValue}: parse a `YYYY-MM-DDTHH:mm` string
 * back into a Date in the user's local timezone. Returns `null` for malformed
 * input so callers can fall back to a default instead of rendering an
 * Invalid Date.
 */
export function fromDateTimeLocalValue(value: string): Date | null {
  const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(value ?? "");
  if (!m) return null;
  const [, y, mo, d, hh, mm] = m;
  const date = new Date(
    Number(y),
    Number(mo) - 1,
    Number(d),
    Number(hh),
    Number(mm),
    0,
    0,
  );
  return Number.isNaN(date.getTime()) ? null : date;
}
