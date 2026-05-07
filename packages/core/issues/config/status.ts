import type { IssueStatus } from "../../types";

// PUL-13 status-flow rework (rev 2.2):
//   New lifecycle: backlog → todo → in_progress ⇄ waiting → planned → developing → deployed
//   Side: blocked, cancelled (manual from any status)
//   Legacy in_review/done remain in the union for the 30-day grace period
//   between PR1 (this) and PR7 (cleanup migration).

export const STATUS_ORDER: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "waiting",
  "planned",
  "developing",
  "deployed",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

export const ALL_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "waiting",
  "planned",
  "developing",
  "deployed",
  "in_review",
  "done",
  "blocked",
  "cancelled",
];

/**
 * Statuses shown as board columns (excludes cancelled — and excludes legacy
 * in_review/done after PR3 lands the v2 board UI behind feature flag
 * issue_status_flow_v2_ui; until then in_review/done remain visible so
 * existing data has a column).
 */
export const BOARD_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "in_progress",
  "waiting",
  "planned",
  "developing",
  "deployed",
  "in_review",
  "done",
  "blocked",
];

export const STATUS_CONFIG: Record<
  IssueStatus,
  {
    label: string;
    iconColor: string;
    hoverBg: string;
    dividerColor: string;
    columnBg: string;
  }
> = {
  backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  // PUL-13: palette A. waiting=warning (needs human reaction), planned=muted (queue),
  // developing=info (active code, distinct from in_progress agent-active warning),
  // deployed=success (terminal good).
  waiting: { label: "Waiting", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", columnBg: "bg-warning/5" },
  planned: { label: "Planned", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  developing: { label: "Developing", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  deployed: { label: "Deployed", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", columnBg: "bg-destructive/5" },
  cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
};
