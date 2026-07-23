import type { OperatingPeriod, OperatingPeriodInput } from "./types";

const MONTH_NAMES = ["January", "February", "March", "April", "May", "June", "July", "August", "September", "October", "November", "December"];

const pad2 = (value: number) => String(value).padStart(2, "0");
const toIso = (year: number, month: number, day: number) => `${year}-${pad2(month)}-${pad2(day)}`;
const daysInMonth = (year: number, month: number) => new Date(Date.UTC(year, month, 0)).getUTCDate();

function parseIso(value: string) {
  const parts = value.slice(0, 10).split("-");
  return { year: Number(parts[0] ?? 0), month: Number(parts[1] ?? 1), day: Number(parts[2] ?? 1) };
}

const monthName = (month: number) => MONTH_NAMES[month - 1] ?? "";

function addDays(value: string, days: number) {
  const { year, month, day } = parseIso(value);
  const date = new Date(Date.UTC(year, month - 1, day + days));
  return toIso(date.getUTCFullYear(), date.getUTCMonth() + 1, date.getUTCDate());
}

function diffDays(from: string, to: string) {
  const a = parseIso(from);
  const b = parseIso(to);
  return Math.round((Date.UTC(b.year, b.month - 1, b.day) - Date.UTC(a.year, a.month - 1, a.day)) / 86_400_000);
}

export function formatPeriodRange(period: { starts_on: string; ends_on: string }): string {
  const fmt = (value: string) => {
    const { year, month, day } = parseIso(value);
    return `${monthName(month).slice(0, 3)} ${day}, ${year}`;
  };
  return `${fmt(period.starts_on)} – ${fmt(period.ends_on)}`;
}

export function latestPeriod(periods: OperatingPeriod[]): OperatingPeriod | null {
  return periods.reduce<OperatingPeriod | null>((latest, period) => (!latest || period.ends_on > latest.ends_on ? period : latest), null);
}

/** Compute the period that follows the latest defined one, so users can plan ahead from any period picker. */
export function nextPeriodInput(periods: OperatingPeriod[], today = new Date().toISOString().slice(0, 10)): OperatingPeriodInput {
  const latest = latestPeriod(periods);
  if (!latest) {
    const { year, month } = parseIso(today);
    const quarter = Math.floor((month - 1) / 3);
    const startMonth = quarter * 3 + 1;
    const endMonth = startMonth + 2;
    return { name: `Q${quarter + 1} ${year}`, unit: "quarter", starts_on: toIso(year, startMonth, 1), ends_on: toIso(year, endMonth, daysInMonth(year, endMonth)) };
  }
  const end = parseIso(latest.ends_on);
  if (latest.unit === "month") {
    const year = end.month === 12 ? end.year + 1 : end.year;
    const month = end.month === 12 ? 1 : end.month + 1;
    return { name: `${monthName(month)} ${year}`, unit: "month", starts_on: toIso(year, month, 1), ends_on: toIso(year, month, daysInMonth(year, month)) };
  }
  if (latest.unit === "custom") {
    const starts = addDays(latest.ends_on, 1);
    const ends = addDays(starts, diffDays(latest.starts_on, latest.ends_on));
    return { name: `${formatPeriodRange({ starts_on: starts, ends_on: ends })}`, unit: "custom", starts_on: starts, ends_on: ends };
  }
  const startMonthRaw = end.month + 1;
  const startYear = startMonthRaw > 12 ? end.year + 1 : end.year;
  const startMonth = startMonthRaw > 12 ? 1 : startMonthRaw;
  const endMonthRaw = startMonth + 2;
  const endYear = endMonthRaw > 12 ? startYear + 1 : startYear;
  const endMonth = endMonthRaw > 12 ? endMonthRaw - 12 : endMonthRaw;
  const quarter = Math.floor((startMonth - 1) / 3) + 1;
  return { name: `Q${quarter} ${startYear}`, unit: "quarter", starts_on: toIso(startYear, startMonth, 1), ends_on: toIso(endYear, endMonth, daysInMonth(endYear, endMonth)) };
}
