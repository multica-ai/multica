import type { DefaultIssueStatus, IssueStatus } from "../../types";

// ---------------------------------------------------------------------------
// StatusDefinition — shape returned by the workspace issue-statuses API.
// ---------------------------------------------------------------------------

export interface StatusDefinition {
  name: string;
  label: string;
  color: string;
  category: 'not_started' | 'started' | 'completed' | 'cancelled';
  position: number;
  isDefault: boolean;
}

// ---------------------------------------------------------------------------
// Built-in default status lists.
// ---------------------------------------------------------------------------

export const DEFAULT_STATUS_ORDER: DefaultIssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export const DEFAULT_ALL_STATUSES: DefaultIssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

/** Statuses shown as board columns (excludes cancelled). */
export const DEFAULT_BOARD_STATUSES: DefaultIssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "done",
  "blocked",
];

export interface StatusConfigEntry {
  label: string;
  iconColor: string;
  hoverBg: string;
  dividerColor: string;
  columnBg: string;
}

export const DEFAULT_STATUS_CONFIG: Record<DefaultIssueStatus, StatusConfigEntry> = {
  backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", columnBg: "bg-destructive/5" },
  cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
};

// ---------------------------------------------------------------------------
// Backward-compatible aliases — existing imports keep working.
// ---------------------------------------------------------------------------

/** @deprecated Use DEFAULT_STATUS_ORDER for clarity. */
export const STATUS_ORDER: IssueStatus[] = DEFAULT_STATUS_ORDER;
/** @deprecated Use DEFAULT_ALL_STATUSES for clarity. */
export const ALL_STATUSES: IssueStatus[] = DEFAULT_ALL_STATUSES;
/** @deprecated Use DEFAULT_BOARD_STATUSES for clarity. */
export const BOARD_STATUSES: IssueStatus[] = DEFAULT_BOARD_STATUSES;
/** @deprecated Use DEFAULT_STATUS_CONFIG for clarity. */
export const STATUS_CONFIG: Record<string, StatusConfigEntry> = DEFAULT_STATUS_CONFIG;
