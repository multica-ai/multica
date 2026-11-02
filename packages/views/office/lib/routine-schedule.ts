import { ROUTINE_CADENCES, type RoutineCadence } from "@multica/core/office";

/**
 * The cron each cadence preset saves. The server owns the mapping (it turns a
 * preset into this expression); the panel needs it only to name a routine's
 * schedule by its preset instead of printing the expression.
 */
export const ROUTINE_PRESET_CRON: Record<RoutineCadence, string> = {
  daily: "0 9 * * *",
  weekdays: "0 9 * * 1-5",
  weekly: "0 9 * * 1",
  biweekly: "0 9 1,15 * *",
  monthly: "0 9 1 * *",
};

/** The preset a cron expression was created from, or null for a custom one. */
export function cadenceOfCron(cron: string): RoutineCadence | null {
  const trimmed = cron.trim();
  return ROUTINE_CADENCES.find((cadence) => ROUTINE_PRESET_CRON[cadence] === trimmed) ?? null;
}
