const DAY_MS = 24 * 60 * 60 * 1000;

function startOfDay(date: Date): number {
  return new Date(date.getFullYear(), date.getMonth(), date.getDate()).getTime();
}

/**
 * When a snooze ends, as short as the distance allows: the time alone today,
 * "tomorrow" and the time, a weekday within the week, a date after that. In
 * the viewer's own time zone, which is the one they picked the time in.
 */
export function formatSnoozeTime(until: Date, now: Date, locale: string): string {
  const time = new Intl.DateTimeFormat(locale, { hour: "numeric", minute: "2-digit" }).format(until);
  const days = Math.round((startOfDay(until) - startOfDay(now)) / DAY_MS);
  if (days <= 0) return time;
  if (days === 1) {
    const tomorrow = new Intl.RelativeTimeFormat(locale, { numeric: "auto" }).format(1, "day");
    return `${tomorrow} ${time}`;
  }
  if (days < 7) {
    const weekday = new Intl.DateTimeFormat(locale, { weekday: "short" }).format(until);
    return `${weekday} ${time}`;
  }
  return new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "numeric",
    minute: "2-digit",
    ...(until.getFullYear() === now.getFullYear() ? {} : { year: "numeric" as const }),
  }).format(until);
}
