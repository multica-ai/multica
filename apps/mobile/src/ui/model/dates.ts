import type { Strings } from "../strings";

const pad = (n: number) => String(n).padStart(2, "0");

/** A calendar day ("YYYY-MM-DD") as a local Date at midnight. */
export function parseDay(day: string): Date | null {
  const match = /^(\d{4})-(\d{2})-(\d{2})/.exec(day);
  if (!match) return null;
  return new Date(Number(match[1]), Number(match[2]) - 1, Number(match[3]));
}

/** A local Date as a calendar day ("YYYY-MM-DD"), the way projects store dates. */
export function toDay(date: Date): string {
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())}`;
}

/** "Nov 30", with the year once it isn't this year. */
export function formatDay(day: string, strings: Strings, now: Date = new Date()): string {
  const date = parseDay(day);
  if (!date) return day;
  return date.getFullYear() === now.getFullYear()
    ? strings.when.day(date.getMonth(), date.getDate())
    : strings.when.dayYear(date.getFullYear(), date.getMonth(), date.getDate());
}

/** "Today 09:00", "Tomorrow 09:00" or "Nov 30 09:00", in the device's time zone. */
export function formatMoment(iso: string, strings: Strings, now: Date = new Date()): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return "";
  const time = `${pad(date.getHours())}:${pad(date.getMinutes())}`;
  const startOfToday = new Date(now.getFullYear(), now.getMonth(), now.getDate()).getTime();
  const dayOffset = Math.floor((date.getTime() - startOfToday) / 86_400_000);
  if (dayOffset === 0) return `${strings.when.today} ${time}`;
  if (dayOffset === 1) return `${strings.when.tomorrow} ${time}`;
  return `${formatDay(toDay(date), strings, now)} ${time}`;
}
