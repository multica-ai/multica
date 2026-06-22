// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — shared source of truth for
// the My Issues date-condition builder. Both the UI (issues-header) and the
// client-side matcher (filter.ts) import from here so a preset's window is
// computed in exactly one place and stays DYNAMIC: named presets re-derive
// their [from,to] from "today" every time, custom relative ranges re-derive via
// relativeDateWindow(), and only an absolute calendar range is fixed.

import {
  addDaysDateOnly,
  relativeDateWindow,
  todayDateOnly,
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

export const DATE_PRESET_LABEL_KEY: Record<
  Exclude<IssueDatePreset, "custom">,
  | "date_today"
  | "date_last_3_days"
  | "date_last_7_days"
  | "date_due_overdue"
  | "date_due_this_week"
  | "date_due_none"
> = {
  today: "date_today",
  last_3_days: "date_last_3_days",
  last_7_days: "date_last_7_days",
  overdue: "date_due_overdue",
  this_week: "date_due_this_week",
  none: "date_due_none",
};

// Named relative presets expressed as relative specs, so they re-derive
// dynamically through the same path as custom relative ranges.
const PRESET_SPEC: Partial<Record<IssueDatePreset, RelativeDateSpec>> = {
  today: { direction: "past", amount: 1, unit: "day" },
  last_3_days: { direction: "past", amount: 3, unit: "day" },
  last_7_days: { direction: "past", amount: 7, unit: "day" },
  this_week: { direction: "next", amount: 7, unit: "day" },
};

/**
 * Build a concrete IssueDateFilter for a preset, computed against "today".
 * Relative/this-week presets attach a `relative` spec so they re-derive on
 * every match. Overdue is open-ended (everything strictly before today) and is
 * re-derived at match time from its preset id. "none" is the empty-due-date
 * mode. The stored from/to are a last-known snapshot for display.
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
  switch (preset) {
    case "overdue":
      return { field: "due_date", from: "1970-01-01", to: addDaysDateOnly(-1), preset };
    case "none":
      return { field: "due_date", from: todayDateOnly(), to: todayDateOnly(), mode: "none", preset };
    default:
      return { field, from: todayDateOnly(), to: todayDateOnly(), preset: "custom" };
  }
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
  if (filter.preset && filter.preset !== "custom" && filter.preset !== "none") {
    const built = buildDatePreset(filter.field, filter.preset);
    return { from: built.from, to: built.to };
  }
  return { from: filter.from, to: filter.to };
}
