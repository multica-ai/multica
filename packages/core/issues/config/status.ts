import type { BuiltInIssueStatus, IssueStatusCategory } from "../../types";

// These three are keyed on CATEGORY, not on status key. A workspace can define
// any number of custom statuses, but every one belongs to exactly one of the
// five lifecycle categories below. Concrete built-in status keys remain a
// separate seven-value compatibility surface. (MUL-6243, MUL-7240)

export const STATUS_ORDER: IssueStatusCategory[] = [
  "backlog",
  "unstarted",
  "started",
  "completed",
  "canceled",
];

export const ALL_STATUSES: IssueStatusCategory[] = [...STATUS_ORDER];

export const BUILT_IN_STATUS_ORDER: BuiltInIssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "in_review",
  "blocked",
  "done",
  "cancelled",
];

export const BUILT_IN_STATUS_CATEGORY: Record<BuiltInIssueStatus, IssueStatusCategory> = {
  backlog: "backlog",
  todo: "unstarted",
  in_progress: "started",
  in_review: "started",
  blocked: "started",
  done: "completed",
  cancelled: "canceled",
};

export const BUILT_IN_STATUS_LABEL: Record<BuiltInIssueStatus, string> = {
  backlog: "Backlog",
  todo: "Todo",
  in_progress: "In Progress",
  in_review: "In Review",
  blocked: "Blocked",
  done: "Done",
  cancelled: "Cancelled",
};

export const STATUS_CONFIG: Record<
  IssueStatusCategory,
  {
    label: string;
    iconColor: string;
    hoverBg: string;
    dividerColor: string;
    columnBg: string;
  }
> = {
  backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  unstarted: { label: "Unstarted", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  started: { label: "Started", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  completed: { label: "Completed", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  canceled: { label: "Canceled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
};
