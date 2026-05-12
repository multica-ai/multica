import type { IssueStatus } from "../../types";

export const STATUS_ORDER: IssueStatus[] = [
  "backlog",
  "todo",
  "planning",
  "ready_for_dev",
  "in_progress",
  "code_review",
  "fixing",
  "testing",
  "coderabbit",
  "resolving",
  "staged",
  "done",
  "blocked",
  "cancelled",
];

export const ALL_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "planning",
  "ready_for_dev",
  "in_progress",
  "code_review",
  "fixing",
  "testing",
  "coderabbit",
  "resolving",
  "staged",
  "done",
  "blocked",
  "cancelled",
];

/** Statuses shown as board columns (excludes cancelled). */
export const BOARD_STATUSES: IssueStatus[] = [
  "backlog",
  "todo",
  "planning",
  "ready_for_dev",
  "in_progress",
  "code_review",
  "fixing",
  "testing",
  "coderabbit",
  "resolving",
  "staged",
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
    badgeBg: string;
    badgeText: string;
    columnBg: string;
  }
> = {
  backlog: { label: "Backlog", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", badgeBg: "bg-muted", badgeText: "text-muted-foreground", columnBg: "bg-muted/40" },
  todo: { label: "Todo", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", badgeBg: "bg-muted", badgeText: "text-muted-foreground", columnBg: "bg-muted/40" },
  planning: { label: "Planning", iconColor: "text-purple-500", hoverBg: "hover:bg-purple-500/10", dividerColor: "bg-purple-500", badgeBg: "bg-purple-500", badgeText: "text-white", columnBg: "bg-purple-500/5" },
  ready_for_dev: { label: "Ready for Dev", iconColor: "text-indigo-500", hoverBg: "hover:bg-indigo-500/10", dividerColor: "bg-indigo-500", badgeBg: "bg-indigo-500", badgeText: "text-white", columnBg: "bg-indigo-500/5" },
  in_progress: { label: "In Progress", iconColor: "text-warning", hoverBg: "hover:bg-warning/10", dividerColor: "bg-warning", badgeBg: "bg-warning", badgeText: "text-white", columnBg: "bg-warning/5" },
  code_review: { label: "Code Review", iconColor: "text-amber-500", hoverBg: "hover:bg-amber-500/10", dividerColor: "bg-amber-500", badgeBg: "bg-amber-500", badgeText: "text-white", columnBg: "bg-amber-500/5" },
  fixing: { label: "Fixing", iconColor: "text-orange-500", hoverBg: "hover:bg-orange-500/10", dividerColor: "bg-orange-500", badgeBg: "bg-orange-500", badgeText: "text-white", columnBg: "bg-orange-500/5" },
  testing: { label: "Testing", iconColor: "text-cyan-500", hoverBg: "hover:bg-cyan-500/10", dividerColor: "bg-cyan-500", badgeBg: "bg-cyan-500", badgeText: "text-white", columnBg: "bg-cyan-500/5" },
  coderabbit: { label: "CodeRabbit", iconColor: "text-pink-500", hoverBg: "hover:bg-pink-500/10", dividerColor: "bg-pink-500", badgeBg: "bg-pink-500", badgeText: "text-white", columnBg: "bg-pink-500/5" },
  resolving: { label: "Resolving", iconColor: "text-violet-500", hoverBg: "hover:bg-violet-500/10", dividerColor: "bg-violet-500", badgeBg: "bg-violet-500", badgeText: "text-white", columnBg: "bg-violet-500/5" },
  staged: { label: "Staged", iconColor: "text-teal-500", hoverBg: "hover:bg-teal-500/10", dividerColor: "bg-teal-500", badgeBg: "bg-teal-500", badgeText: "text-white", columnBg: "bg-teal-500/5" },
  done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", badgeBg: "bg-info", badgeText: "text-white", columnBg: "bg-info/5" },
  blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", badgeBg: "bg-destructive", badgeText: "text-white", columnBg: "bg-destructive/5" },
  cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", badgeBg: "bg-muted", badgeText: "text-muted-foreground", columnBg: "bg-muted/40" },
};
