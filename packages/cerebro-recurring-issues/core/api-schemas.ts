import { z } from "zod";

// Response readers route through parseWithFallback so an older/newer backend
// degrades to safe defaults instead of breaking the UI (CLAUDE.md → API
// Response Compatibility).

const frequency = z.enum(["daily", "weekly", "monthly", "yearly", "days_after", "custom", "every_weekday"]);
const anchor = z.enum(["completion", "due_date"]);

// FIR-1639 fields. Older backends omit them and newer backends may add enum
// values; both must degrade rather than fail the whole object
// (CLAUDE.md → Enum drift downgrades, not crashes). .default() covers a
// missing key, .catch() covers an unknown value.
const triggerType = z.enum(["completion", "schedule"]).catch("completion").default("completion");
const staleHandling = z.enum(["leave_open", "auto_close"]).catch("leave_open").default("leave_open");
const dateFormat = z.enum(["iso", "dk"]).catch("iso").default("iso");

export const issueRecurrenceSchema = z.object({
  id: z.string(),
  workspace_id: z.string(),
  project_id: z.string().optional(),
  source_issue_id: z.string(),
  frequency,
  interval_count: z.number().int().positive(),
  weekdays: z.array(z.number().int()).default([]),
  days_after: z.number().int().min(0),
  trigger_status: z.string(),
  anchor,
  create_new_issue: z.boolean(),
  new_status: z.string(),
  recur_forever: z.boolean(),
  end_date: z.string().optional(),
  max_occurrences: z.number().int().optional(),
  occurrence_count: z.number().int(),
  armed: z.boolean(),
  enabled: z.boolean(),
  trigger_type: triggerType,
  stale_handling: staleHandling,
  stale_status: z.string().catch("cancelled").default("cancelled"),
  date_format: dateFormat,
  next_run_at: z.string().optional(),
  created_at: z.string(),
  updated_at: z.string(),
});

export const runResultSchema = z.object({ spawned: z.boolean() });
export const EMPTY_RUN_RESULT = { spawned: false };
