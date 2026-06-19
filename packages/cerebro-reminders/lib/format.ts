// Danish reminder time formatting for the overview list + opened card.
// Kept pure (an injectable `now`) so it is trivially testable.

function isSameDay(a: Date, b: Date): boolean {
  return (
    a.getFullYear() === b.getFullYear() &&
    a.getMonth() === b.getMonth() &&
    a.getDate() === b.getDate()
  );
}

function addDays(d: Date, days: number): Date {
  const copy = new Date(d);
  copy.setDate(copy.getDate() + days);
  return copy;
}

function capitalize(s: string): string {
  return s.length === 0 ? s : s.charAt(0).toUpperCase() + s.slice(1);
}

const timeFmt = new Intl.DateTimeFormat("da-DK", { hour: "2-digit", minute: "2-digit" });
const weekdayFmt = new Intl.DateTimeFormat("da-DK", { weekday: "short" });
const dateFmt = new Intl.DateTimeFormat("da-DK", { day: "numeric", month: "short" });

/**
 * Short label for a reminder row: "I dag 14:00", "I morgen 09:00", or
 * "Tor 20. jun" once it is further out than tomorrow.
 */
export function formatReminderWhen(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const time = timeFmt.format(d);
  if (isSameDay(d, now)) return `I dag ${time}`;
  if (isSameDay(d, addDays(now, 1))) return `I morgen ${time}`;
  const weekday = capitalize(weekdayFmt.format(d).replace(/\.$/, ""));
  return `${weekday} ${dateFmt.format(d)}`;
}

/**
 * Long label for the opened card: "Forfalder i dag kl. 14:00" /
 * "Forfalder i morgen kl. 09:00" / "Forfalder tor. 20. jun kl. 14:00".
 */
export function formatReminderDue(iso: string, now: Date = new Date()): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "";
  const time = timeFmt.format(d);
  if (isSameDay(d, now)) return `Forfalder i dag kl. ${time}`;
  if (isSameDay(d, addDays(now, 1))) return `Forfalder i morgen kl. ${time}`;
  return `Forfalder ${weekdayFmt.format(d)} ${dateFmt.format(d)} kl. ${time}`;
}

/**
 * Source line for a reminder: "DM · Filip", "#kampagner", or the conversation
 * title for a regular issue.
 */
export function formatReminderSource(kind?: string, title?: string): string {
  const t = (title ?? "").trim();
  if (kind === "dm") return t ? `DM · ${t}` : "Direkte besked";
  if (kind === "channel") return t ? `#${t}` : "Kanal";
  return t || "Besked";
}
