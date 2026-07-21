import type { MeetingCadenceUnit } from "./types";

export type CycleRelative = "past" | "current" | "upcoming";

export interface CycleOccurrence {
  /** Integer step from the current cycle: 0 = current, negative = past, positive = upcoming. */
  offset: number;
  /** ISO yyyy-mm-dd start date of the cycle. */
  date: string;
  relative: CycleRelative;
}

const MONTH_ABBR = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
const pad2 = (value: number) => String(value).padStart(2, "0");
const toIso = (year: number, month: number, day: number) => `${year}-${pad2(month)}-${pad2(day)}`;

function parseIso(value: string) {
  const parts = value.slice(0, 10).split("-");
  return { year: Number(parts[0] ?? 0), month: Number(parts[1] ?? 1), day: Number(parts[2] ?? 1) };
}

/** Advance an ISO date by `steps` cadence intervals. Negative steps go back in time. */
export function shiftByCadence(iso: string, unit: MeetingCadenceUnit, count: number, steps: number): string {
  const { year, month, day } = parseIso(iso);
  const magnitude = count * steps;
  if (unit === "day" || unit === "week") {
    const days = unit === "week" ? magnitude * 7 : magnitude;
    const date = new Date(Date.UTC(year, month - 1, day + days));
    return toIso(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate());
  }
  const months = unit === "quarter" ? magnitude * 3 : magnitude;
  const date = new Date(Date.UTC(year, month - 1 + months, day));
  return toIso(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate());
}

export function formatCycleDate(iso: string): string {
  const { year, month, day } = parseIso(iso);
  return `${MONTH_ABBR[month - 1] ?? ""} ${day}, ${year}`;
}

/**
 * Project the recurring cycle schedule around today so a user can plan the timing.
 * Returns an empty timeline when there is no recurring cadence (manual or invalid count).
 */
export function buildCycleTimeline(
  unit: MeetingCadenceUnit,
  count: number,
  today = new Date().toISOString().slice(0, 10),
  window: { past?: number; upcoming?: number } = {},
): CycleOccurrence[] {
  if (unit === "manual" || count < 1) return [];
  const past = Math.max(0, window.past ?? 1);
  const upcoming = Math.max(0, window.upcoming ?? 5);
  const occurrences: CycleOccurrence[] = [];
  for (let offset = -past; offset <= upcoming; offset++) {
    occurrences.push({
      offset,
      date: shiftByCadence(today, unit, count, offset),
      relative: offset < 0 ? "past" : offset === 0 ? "current" : "upcoming",
    });
  }
  return occurrences;
}
