// Client-side port of the sprint name-template + cadence date math that the
// server applies when it auto-creates the next sprint
// (server/internal/cerebro/sprints/service.go). The manual "New sprint" dialog
// reuses this so a hand-created sprint follows the same Name template and
// cadence as an auto-created one (FIR-1581). Kept pure so it can be unit-tested
// in isolation and shared by any future preview surface.

import type { CadenceUnit } from "./types";

const DATE_LAYOUT_RE = /^\d{4}-\d{2}-\d{2}$/;

/** Parse a YYYY-MM-DD string into a UTC-midnight Date. Returns null if malformed. */
export function parseDateOnly(value: string): Date | null {
  if (!DATE_LAYOUT_RE.test(value)) return null;
  const parts = value.split("-").map(Number);
  const y = parts[0]!;
  const m = parts[1]!;
  const d = parts[2]!;
  const date = new Date(Date.UTC(y, m - 1, d));
  // Guard against rollover (e.g. 2026-02-31 → March) so callers get null, not silent drift.
  if (date.getUTCFullYear() !== y || date.getUTCMonth() !== m - 1 || date.getUTCDate() !== d) {
    return null;
  }
  return date;
}

/** Format a UTC Date back to YYYY-MM-DD. */
export function formatDateOnly(date: Date): string {
  const y = date.getUTCFullYear().toString().padStart(4, "0");
  const m = (date.getUTCMonth() + 1).toString().padStart(2, "0");
  const d = date.getUTCDate().toString().padStart(2, "0");
  return `${y}-${m}-${d}`;
}

function isoWeekday(date: Date): number {
  const w = date.getUTCDay(); // Sun=0..Sat=6
  return w === 0 ? 7 : w;
}

function normalizeIsoWeekday(w: number): number {
  if (w < 1) return 1;
  if (w > 7) return 7;
  return w;
}

/**
 * Derive the start date of the sprint following `previousEnd` (inclusive end of
 * the previous sprint). Mirrors ComputeNextStart in service.go: week-cadence
 * snaps onto startWeekday, month-cadence snaps to the 1st of the next month.
 */
export function computeNextStart(
  previousEnd: Date,
  unit: CadenceUnit,
  startWeekday: number,
): Date {
  const day = new Date(
    Date.UTC(previousEnd.getUTCFullYear(), previousEnd.getUTCMonth(), previousEnd.getUTCDate() + 1),
  );
  switch (unit) {
    case "week": {
      const current = isoWeekday(day);
      const target = normalizeIsoWeekday(startWeekday);
      const offset = (target - current + 7) % 7;
      return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), day.getUTCDate() + offset));
    }
    case "month":
      return new Date(Date.UTC(day.getUTCFullYear(), day.getUTCMonth(), 1));
    default:
      return day;
  }
}

/**
 * Inclusive end date of a sprint that starts on `start` with the given cadence.
 * Mirrors ComputeEnd in service.go: end = start + N units - 1 day.
 */
export function computeEnd(start: Date, unit: CadenceUnit, count: number): Date {
  const n = count > 0 ? count : 1;
  let end: Date;
  switch (unit) {
    case "week":
      end = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate() + 7 * n));
      break;
    case "month":
      end = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth() + n, start.getUTCDate()));
      break;
    default:
      end = new Date(Date.UTC(start.getUTCFullYear(), start.getUTCMonth(), start.getUTCDate() + n));
      break;
  }
  return new Date(Date.UTC(end.getUTCFullYear(), end.getUTCMonth(), end.getUTCDate() - 1));
}

/**
 * Substitute {n}, {start}, {end} in the template. Mirrors
 * ApplyNameTemplateWithDates in service.go, including the no-placeholder
 * fallback that appends the sequence so sprints never share a name.
 *
 * Dates are optional YYYY-MM-DD strings — {start}/{end} are only substituted
 * when a valid date is supplied (matching the Go `!start.IsZero()` guard).
 */
export function applyNameTemplate(
  template: string,
  sequenceNo: number,
  start?: string,
  end?: string,
): string {
  let out = template;
  let replaced = false;
  if (out.includes("{n}")) {
    out = out.replaceAll("{n}", String(sequenceNo));
    replaced = true;
  }
  if (start && parseDateOnly(start) && out.includes("{start}")) {
    out = out.replaceAll("{start}", start);
    replaced = true;
  }
  if (end && parseDateOnly(end) && out.includes("{end}")) {
    out = out.replaceAll("{end}", end);
    replaced = true;
  }
  if (replaced) return out;
  if (out === "") return `Sprint ${sequenceNo}`;
  return `${out} ${sequenceNo}`;
}
