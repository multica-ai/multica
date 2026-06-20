// TECH-3064 / FIR-334 recurring issues — shared types. Mirror the backend
// RecurrenceResponse (server/internal/cerebro/recurringissue/types.go).

export type Frequency = "daily" | "weekly" | "monthly" | "yearly" | "days_after" | "custom" | "every_weekday";
export const FREQUENCIES: Frequency[] = [
  "daily",
  "weekly",
  "monthly",
  "yearly",
  "days_after",
  "custom",
  "every_weekday",
];

export type Anchor = "completion" | "due_date";

/** FIR-1639 — what makes the rule fire. */
export type TriggerType = "completion" | "schedule";

/** FIR-1639 — for schedule rules, what happens to a still-open previous
 * occurrence when the schedule fires again. */
export type StaleHandling = "leave_open" | "auto_close";

/** FIR-1639 — format used to substitute the {{date}} title placeholder. */
export type DateFormat = "iso" | "dk";

/** The literal token a user puts in a title to date-stamp each occurrence. */
export const DATE_PLACEHOLDER = "{{date}}";

export const DATE_FORMATS: { value: DateFormat; label: string }[] = [
  { value: "iso", label: "2026-06-20" },
  { value: "dk", label: "20-06-2026" },
];

/** ISO weekday 1=Mon..7=Sun, paired with a short label for the picker. */
export const WEEKDAYS: { value: number; label: string }[] = [
  { value: 1, label: "Mo" },
  { value: 2, label: "Tu" },
  { value: 3, label: "We" },
  { value: 4, label: "Th" },
  { value: 5, label: "Fr" },
  { value: 6, label: "Sa" },
  { value: 7, label: "Su" },
];

/** Issue statuses available for trigger_status / new_status pickers. */
export const ISSUE_STATUSES: string[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export interface IssueRecurrence {
  id: string;
  workspace_id: string;
  project_id?: string;
  source_issue_id: string;
  frequency: Frequency;
  interval_count: number;
  weekdays: number[];
  days_after: number;
  trigger_status: string;
  anchor: Anchor;
  create_new_issue: boolean;
  new_status: string;
  recur_forever: boolean;
  end_date?: string;
  max_occurrences?: number;
  occurrence_count: number;
  armed: boolean;
  enabled: boolean;
  trigger_type: TriggerType;
  stale_handling: StaleHandling;
  stale_status: string;
  date_format: DateFormat;
  next_run_at?: string;
  created_at: string;
  updated_at: string;
}

export interface RecurrenceWriteInput {
  frequency?: Frequency;
  interval_count?: number;
  weekdays?: number[];
  days_after?: number;
  trigger_status?: string;
  anchor?: Anchor;
  create_new_issue?: boolean;
  new_status?: string;
  recur_forever?: boolean;
  end_date?: string;
  max_occurrences?: number;
  enabled?: boolean;
  trigger_type?: TriggerType;
  stale_handling?: StaleHandling;
  stale_status?: string;
  date_format?: DateFormat;
}
