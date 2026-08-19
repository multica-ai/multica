// Minimal date-only helpers for the "Last activity" range filter (plan §3.2).
// Modeled on packages/core/issues/date.ts's convention — a calendar day
// picked from a <Calendar> is a LOCAL date, not an instant; serializing via
// toISOString() would shift the day across a UTC boundary (the bug class
// behind GH #3618 / MUL-2925). Not importing that helper directly: apps/admin
// deliberately avoids depending on @multica/core for a single filter (see
// lib/schema.ts's parseWithFallback comment) — same reasoning applies here.

const DATE_ONLY = /^(\d{4})-(\d{2})-(\d{2})/;

function pad(n: number): string {
  return String(n).padStart(2, "0");
}

/** Serialize a Date (local midnight of the chosen day) to "YYYY-MM-DD". */
export function toDateOnly(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** Parse a "YYYY-MM-DD" string into a Date at local midnight of that day. */
export function dateOnlyToLocalDate(value: string | null | undefined): Date | undefined {
  if (!value) return undefined;
  const m = DATE_ONLY.exec(value);
  if (!m) return undefined;
  return new Date(Number(m[1]), Number(m[2]) - 1, Number(m[3]));
}

/** Short display label, e.g. "Mar 1". Falls back to the raw string if unparseable. */
export function formatDateOnlyShort(value: string): string {
  const d = dateOnlyToLocalDate(value);
  return d ? d.toLocaleDateString(undefined, { month: "short", day: "numeric" }) : value;
}
