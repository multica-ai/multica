// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 / FIR-1812 — shared source of
// truth for the My Issues date-condition builder. Both the UI (issues-header)
// and the client-side matcher (filter.ts) import from here so a preset's window
// is computed in exactly one place and stays DYNAMIC: named presets re-derive
// their [from,to] from "today" every time, custom relative ranges re-derive via
// relativeDateWindow(), and only an absolute calendar range is fixed.
//
// FIR-1812 widened the value vocabulary to match the reference filter UI
// (Linear/ClickUp-style): calendar-aligned week/month/quarter/year presets,
// open-ended "Today & earlier" / "Later than today" / "Overdue", "Is set" /
// "Is not set", and absolute on/before/after/range. The matcher stays a single
// window compare plus the two set-modes; ALL the per-preset window math lives
// here.

import {
  addDaysDateOnly,
  relativeDateWindow,
  todayDateOnly,
  toDateOnly,
} from "@multica/core/issues/date";
import type {
  IssueDateField,
  IssueDateFilter,
  IssueDatePreset,
} from "@multica/core/issues/stores/view-store";
import type { RelativeDateSpec } from "@multica/core/issues/date";

// Relative presets honour the chosen field (created/updated/due); due presets
// always pin field=due_date.
export const RELATIVE_PRESETS = ["today", "last_3_days", "last_7_days"] as const;
export const DUE_PRESETS = ["overdue", "this_week", "none"] as const;

// i18n label keys. created/updated/due field labels and preset labels.
export const DATE_FIELD_LABEL_KEY: Record<
  IssueDateField,
  "date_field_created" | "date_field_updated" | "date_field_due"
> = {
  created_at: "date_field_created",
  updated_at: "date_field_updated",
  due_date: "date_field_due",
};

// Literal i18n keys (under issues.json "filters") for every date label the
// builders render. Kept as a literal union so the typed t($ => $.filters[key])
// accessor accepts them — indexing with a plain `string` would not typecheck.
export type DateLabelKey =
  | "date_today"
  | "date_yesterday"
  | "date_tomorrow"
  | "date_last_3_days"
  | "date_last_7_days"
  | "date_this_week"
  | "date_last_week"
  | "date_next_week"
  | "date_this_month"
  | "date_last_month"
  | "date_next_month"
  | "date_this_quarter"
  | "date_last_quarter"
  | "date_next_quarter"
  | "date_this_year"
  | "date_last_year"
  | "date_next_year"
  | "date_due_overdue"
  | "date_today_and_earlier"
  | "date_later_than_today"
  | "date_due_none"
  | "date_any"
  | "date_value_last_n"
  | "date_value_next_n"
  | "date_value_on"
  | "date_value_before"
  | "date_value_after"
  | "date_value_range";

// Label keys for EVERY named preset. The v1 builder reads the subset it offers;
// the FIR-1812 builder reads the full list. Keep this exhaustive so a condition
// always renders a readable value name instead of a raw from..to range.
export const DATE_PRESET_LABEL_KEY: Record<
  Exclude<IssueDatePreset, "custom">,
  DateLabelKey
> = {
  today: "date_today",
  yesterday: "date_yesterday",
  tomorrow: "date_tomorrow",
  last_3_days: "date_last_3_days",
  last_7_days: "date_last_7_days",
  this_week: "date_this_week",
  last_week: "date_last_week",
  next_week: "date_next_week",
  this_month: "date_this_month",
  last_month: "date_last_month",
  next_month: "date_next_month",
  this_quarter: "date_this_quarter",
  last_quarter: "date_last_quarter",
  next_quarter: "date_next_quarter",
  this_year: "date_this_year",
  last_year: "date_last_year",
  next_year: "date_next_year",
  overdue: "date_due_overdue",
  today_and_earlier: "date_today_and_earlier",
  later_than_today: "date_later_than_today",
  none: "date_due_none",
  any: "date_any",
};

// Open-ended sentinels — a window that reaches to the start/end of time. The
// matcher does an inclusive [from,to] compare, so these make "before X" /
// "after X" / "overdue" one-sided without a special code path.
const FAR_PAST = "1970-01-01";
const FAR_FUTURE = "9999-12-31";

// --- Local calendar-boundary helpers (operate on a local Date, emit a
// date-only string). Weeks are Monday-based to match the European/Linear
// convention. ---
function startOfWeek(d: Date): Date {
  const x = new Date(d.getFullYear(), d.getMonth(), d.getDate());
  const back = (x.getDay() + 6) % 7; // 0 = Monday
  x.setDate(x.getDate() - back);
  return x;
}
function endOfWeek(d: Date): Date {
  const s = startOfWeek(d);
  return new Date(s.getFullYear(), s.getMonth(), s.getDate() + 6);
}
function monthWindow(year: number, monthIndex: number): { from: string; to: string } {
  return {
    from: toDateOnly(new Date(year, monthIndex, 1)),
    to: toDateOnly(new Date(year, monthIndex + 1, 0)),
  };
}
function quarterWindow(year: number, monthIndex: number): { from: string; to: string } {
  // Normalize through Date so a negative/overflowing monthIndex rolls the year
  // correctly (e.g. m-3 in Jan → Oct of the previous year).
  const ref = new Date(year, monthIndex, 1);
  const q = Math.floor(ref.getMonth() / 3) * 3;
  return {
    from: toDateOnly(new Date(ref.getFullYear(), q, 1)),
    to: toDateOnly(new Date(ref.getFullYear(), q + 3, 0)),
  };
}
function yearWindow(year: number): { from: string; to: string } {
  return {
    from: toDateOnly(new Date(year, 0, 1)),
    to: toDateOnly(new Date(year, 11, 31)),
  };
}

// Named relative presets expressed as relative specs, so they re-derive
// dynamically through the same path as custom relative ranges. (Calendar-period
// presets are handled in namedWindow() instead — they are not fixed-length.)
const PRESET_SPEC: Partial<Record<IssueDatePreset, RelativeDateSpec>> = {
  today: { direction: "past", amount: 1, unit: "day" },
  last_3_days: { direction: "past", amount: 3, unit: "day" },
  last_7_days: { direction: "past", amount: 7, unit: "day" },
};

// Re-derive the [from,to] window for a NAMED preset from "today". This is the
// single place calendar math lives; the matcher calls it through
// resolveDateWindow(). Returns null for "custom"/"none"/"any" (handled by the
// matcher's set-modes / stored range instead).
export function namedWindow(preset: IssueDatePreset): { from: string; to: string } | null {
  const now = new Date();
  const y = now.getFullYear();
  const m = now.getMonth();
  switch (preset) {
    case "today":
      return { from: todayDateOnly(), to: todayDateOnly() };
    case "yesterday":
      return { from: addDaysDateOnly(-1), to: addDaysDateOnly(-1) };
    case "tomorrow":
      return { from: addDaysDateOnly(1), to: addDaysDateOnly(1) };
    case "last_3_days":
      return relativeDateWindow({ direction: "past", amount: 3, unit: "day" });
    case "last_7_days":
      return relativeDateWindow({ direction: "past", amount: 7, unit: "day" });
    case "this_week":
      return { from: toDateOnly(startOfWeek(now)), to: toDateOnly(endOfWeek(now)) };
    case "last_week": {
      const d = new Date(y, m, now.getDate() - 7);
      return { from: toDateOnly(startOfWeek(d)), to: toDateOnly(endOfWeek(d)) };
    }
    case "next_week": {
      const d = new Date(y, m, now.getDate() + 7);
      return { from: toDateOnly(startOfWeek(d)), to: toDateOnly(endOfWeek(d)) };
    }
    case "this_month":
      return monthWindow(y, m);
    case "last_month":
      return monthWindow(y, m - 1);
    case "next_month":
      return monthWindow(y, m + 1);
    case "this_quarter":
      return quarterWindow(y, m);
    case "last_quarter":
      return quarterWindow(y, m - 3);
    case "next_quarter":
      return quarterWindow(y, m + 3);
    case "this_year":
      return yearWindow(y);
    case "last_year":
      return yearWindow(y - 1);
    case "next_year":
      return yearWindow(y + 1);
    case "overdue":
      return { from: FAR_PAST, to: addDaysDateOnly(-1) };
    case "today_and_earlier":
      return { from: FAR_PAST, to: todayDateOnly() };
    case "later_than_today":
      return { from: addDaysDateOnly(1), to: FAR_FUTURE };
    default:
      return null;
  }
}

/**
 * Build a concrete IssueDateFilter for a preset, computed against "today".
 * Rolling day presets attach a `relative` spec so they re-derive on every
 * match through the same path as custom relative ranges. Calendar-period and
 * open-ended presets carry a last-known [from,to] snapshot and re-derive via
 * namedWindow() at match time. "none"/"any" are the set-modes (no due date /
 * any date). The stored from/to are a display cache for anything dynamic.
 */
export function buildDatePreset(
  field: IssueDateField,
  preset: IssueDatePreset,
): IssueDateFilter {
  const spec = PRESET_SPEC[preset];
  if (spec) {
    const win = relativeDateWindow(spec);
    return { field, from: win.from, to: win.to, preset, relative: spec };
  }
  if (preset === "none") {
    return { field: "due_date", from: todayDateOnly(), to: todayDateOnly(), mode: "none", preset };
  }
  if (preset === "any") {
    return { field, from: FAR_PAST, to: FAR_FUTURE, mode: "set", preset };
  }
  const win = namedWindow(preset);
  if (win) {
    // Overdue is a due-date concept; the rest honour the chosen field.
    return { field: preset === "overdue" ? "due_date" : field, from: win.from, to: win.to, preset };
  }
  return { field, from: todayDateOnly(), to: todayDateOnly(), preset: "custom" };
}

/**
 * Resolve a filter to its effective [from, to] window, re-derived from "today"
 * for anything dynamic. This is the single function the matcher calls.
 * - relative spec present → recompute from now.
 * - named non-custom preset → recompute that preset from now.
 * - otherwise (absolute custom range) → the stored from/to.
 */
export function resolveDateWindow(filter: IssueDateFilter): {
  from: string;
  to: string;
} {
  if (filter.relative) return relativeDateWindow(filter.relative);
  if (
    filter.preset &&
    filter.preset !== "custom" &&
    filter.preset !== "none" &&
    filter.preset !== "any"
  ) {
    const win = namedWindow(filter.preset);
    if (win) return win;
  }
  return { from: filter.from, to: filter.to };
}

// --- FIR-1812 value vocabulary -------------------------------------------
// The reference filter offers a searchable value list. We model it as an
// ordered, grouped catalog of "value options". Most are named presets; a few
// are parametric (relative N units, absolute on/before/after/range) and open
// their own inline control. The builder UI renders this catalog verbatim.

export type DateValueKind =
  | "preset" // a fixed named preset (today, this_week, overdue, …)
  | "relative" // Last/Next N days/weeks/months/years (number + unit input)
  | "on" // an exact single calendar day
  | "before" // strictly before a calendar day
  | "after" // strictly after a calendar day
  | "range"; // an absolute from..to calendar range

export interface DateValueOption {
  id: string;
  kind: DateValueKind;
  preset?: IssueDatePreset; // for kind="preset"
  labelKey: DateLabelKey;
  group: "common" | "relative" | "period" | "open" | "absolute";
}

// Ordered to mirror the reference dropdown.
export const DATE_VALUE_OPTIONS: DateValueOption[] = [
  { id: "today", kind: "preset", preset: "today", labelKey: "date_today", group: "common" },
  { id: "yesterday", kind: "preset", preset: "yesterday", labelKey: "date_yesterday", group: "common" },
  { id: "tomorrow", kind: "preset", preset: "tomorrow", labelKey: "date_tomorrow", group: "common" },
  { id: "this_week", kind: "preset", preset: "this_week", labelKey: "date_this_week", group: "period" },
  { id: "last_week", kind: "preset", preset: "last_week", labelKey: "date_last_week", group: "period" },
  { id: "next_week", kind: "preset", preset: "next_week", labelKey: "date_next_week", group: "period" },
  { id: "this_month", kind: "preset", preset: "this_month", labelKey: "date_this_month", group: "period" },
  { id: "last_month", kind: "preset", preset: "last_month", labelKey: "date_last_month", group: "period" },
  { id: "next_month", kind: "preset", preset: "next_month", labelKey: "date_next_month", group: "period" },
  { id: "this_quarter", kind: "preset", preset: "this_quarter", labelKey: "date_this_quarter", group: "period" },
  { id: "last_quarter", kind: "preset", preset: "last_quarter", labelKey: "date_last_quarter", group: "period" },
  { id: "next_quarter", kind: "preset", preset: "next_quarter", labelKey: "date_next_quarter", group: "period" },
  { id: "this_year", kind: "preset", preset: "this_year", labelKey: "date_this_year", group: "period" },
  { id: "last_year", kind: "preset", preset: "last_year", labelKey: "date_last_year", group: "period" },
  { id: "next_year", kind: "preset", preset: "next_year", labelKey: "date_next_year", group: "period" },
  { id: "overdue", kind: "preset", preset: "overdue", labelKey: "date_due_overdue", group: "open" },
  { id: "today_and_earlier", kind: "preset", preset: "today_and_earlier", labelKey: "date_today_and_earlier", group: "open" },
  { id: "later_than_today", kind: "preset", preset: "later_than_today", labelKey: "date_later_than_today", group: "open" },
  { id: "last_n", kind: "relative", labelKey: "date_value_last_n", group: "relative" },
  { id: "next_n", kind: "relative", labelKey: "date_value_next_n", group: "relative" },
  { id: "on", kind: "on", labelKey: "date_value_on", group: "absolute" },
  { id: "before", kind: "before", labelKey: "date_value_before", group: "absolute" },
  { id: "after", kind: "after", labelKey: "date_value_after", group: "absolute" },
  { id: "range", kind: "range", labelKey: "date_value_range", group: "absolute" },
];

// Build an absolute on/before/after filter for a single picked day. "before" /
// "after" are inclusive of the picked day (on-or-before / on-or-after) so the
// picked day round-trips cleanly from the stored edge — the matcher's inclusive
// window does the rest. Stored with an open sentinel on the unbounded side.
export function buildAbsoluteSingle(
  field: IssueDateField,
  kind: "on" | "before" | "after",
  day: string,
): IssueDateFilter {
  if (kind === "on") return { field, from: day, to: day, preset: "custom" };
  if (kind === "before") return { field, from: FAR_PAST, to: day, preset: "custom" };
  return { field, from: day, to: FAR_FUTURE, preset: "custom" };
}

// The single calendar day a stored on/before/after filter was built from.
export function absoluteSingleDay(filter: IssueDateFilter): string {
  return filter.from === FAR_PAST ? filter.to : filter.from;
}

// Which value-option id a stored filter currently represents, so the builder
// can show the right selection when re-opened.
export function valueOptionIdForFilter(filter: IssueDateFilter): string {
  if (filter.mode === "none") return "no_set"; // handled by operator, not value
  if (filter.mode === "set") return "any";
  if (filter.relative) return filter.relative.direction === "past" ? "last_n" : "next_n";
  if (filter.preset && filter.preset !== "custom") return filter.preset;
  // Absolute: distinguish on / before / after / range by the sentinels.
  if (filter.from === FAR_PAST) return "before";
  if (filter.to === FAR_FUTURE) return "after";
  if (filter.from === filter.to) return "on";
  return "range";
}

export { FAR_PAST, FAR_FUTURE };
