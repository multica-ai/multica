/** Render an instant on the schedule's clock, not the reader's: an autopilot
 *  that says "18:00 (America/Los_Angeles)" must not print its next run as 09:00
 *  to a reader in UTC+8. The wording — month name, 12- or 24-hour dial — is the
 *  reader's locale.
 *
 *  Two things can go wrong, and neither may take a page down:
 *  - a zone this browser's ICU data does not carry (an autopilot saved from a
 *    newer build, opened in an older packaged desktop one) → local time: wrong by
 *    an offset, but still a time of day, which a raw ISO string is not;
 *  - a timestamp that is not a date at all (backend drift) → the string itself.
 *    Intl throws on an invalid Date, so this is checked before formatting, not
 *    caught after: a catch would only run the same throwing call again. */
export function formatInTimeZone(
  iso: string,
  timeZone: string | undefined,
  locale: string,
): string {
  const at = new Date(iso);
  if (Number.isNaN(at.getTime())) return iso;
  const options: Intl.DateTimeFormatOptions = {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  };
  try {
    return new Intl.DateTimeFormat(locale, { ...options, timeZone }).format(at);
  } catch {
    return new Intl.DateTimeFormat(locale, options).format(at);
  }
}
