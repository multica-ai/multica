/**
 * Returns the next occurrence of 09:00 in local time, N days from now
 * (default 1 = tomorrow at 09:00).
 */
export function nextLocalNineAm(daysAhead = 1): Date {
  const d = new Date();
  d.setDate(d.getDate() + daysAhead);
  d.setHours(9, 0, 0, 0);
  return d;
}

// nextMondayNineAm returns the coming Monday at 09:00 in local time.
export function nextMondayNineAm(): Date {
  const d = new Date();
  const day = d.getDay();
  const daysUntilMonday = day === 0 ? 1 : 8 - day;
  d.setDate(d.getDate() + daysUntilMonday);
  d.setHours(9, 0, 0, 0);
  return d;
}
