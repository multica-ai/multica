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
}
