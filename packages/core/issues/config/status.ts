import type { IssueStatus } from "../../types";

// Order is the left-to-right column order on the board.
// BMAD statuses are interleaved into the upstream flow at their natural points:
//   backlog -> todo -> planning -> ready_for_dev -> in_progress -> code_review ->
//   fixing -> testing -> coderabbit -> resolving -> in_review -> staged -> done
// blocked + cancelled are side exits.
//
// NOTE: STATUS_ORDER matches the TS IssueStatus union (15 values incl.
// `in_review`). The DB CHECK is a strict subset of these 15: it excludes
// `in_review`, so no row will ever land in that column at runtime. The column
// is rendered but stays empty by design; see /opt/v6_execution_plan.md.
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
  "in_review",
  "staged",
  "done",
  "blocked",
  "cancelled",
];

export const ALL_STATUSES: IssueStatus[] = [...STATUS_ORDER];

/** Statuses shown as board columns (excludes cancelled). */
export const BOARD_STATUSES: IssueStatus[] = STATUS_ORDER.filter(
  (s) => s !== "cancelled",
);

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
  in_review: { label: "In Review", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  done: { label: "Done", iconColor: "text-info", hoverBg: "hover:bg-info/10", dividerColor: "bg-info", columnBg: "bg-info/5" },
  blocked: { label: "Blocked", iconColor: "text-destructive", hoverBg: "hover:bg-destructive/10", dividerColor: "bg-destructive", columnBg: "bg-destructive/5" },
  cancelled: { label: "Cancelled", iconColor: "text-muted-foreground", hoverBg: "hover:bg-accent", dividerColor: "bg-muted-foreground/40", columnBg: "bg-muted/40" },
  planning: { label: "Planning", iconColor: "text-indigo-500", hoverBg: "hover:bg-indigo-500/10", dividerColor: "bg-indigo-500", columnBg: "bg-indigo-500/5" },
  ready_for_dev: { label: "Ready for Dev", iconColor: "text-cyan-500", hoverBg: "hover:bg-cyan-500/10", dividerColor: "bg-cyan-500", columnBg: "bg-cyan-500/5" },
  code_review: { label: "Code Review", iconColor: "text-success", hoverBg: "hover:bg-success/10", dividerColor: "bg-success", columnBg: "bg-success/5" },
  fixing: { label: "Fixing", iconColor: "text-amber-500", hoverBg: "hover:bg-amber-500/10", dividerColor: "bg-amber-500", columnBg: "bg-amber-500/5" },
  testing: { label: "Testing", iconColor: "text-violet-500", hoverBg: "hover:bg-violet-500/10", dividerColor: "bg-violet-500", columnBg: "bg-violet-500/5" },
  coderabbit: { label: "CodeRabbit", iconColor: "text-pink-500", hoverBg: "hover:bg-pink-500/10", dividerColor: "bg-pink-500", columnBg: "bg-pink-500/5" },
  resolving: { label: "Resolving", iconColor: "text-orange-500", hoverBg: "hover:bg-orange-500/10", dividerColor: "bg-orange-500", columnBg: "bg-orange-500/5" },
  staged: { label: "Staged", iconColor: "text-teal-500", hoverBg: "hover:bg-teal-500/10", dividerColor: "bg-teal-500", columnBg: "bg-teal-500/5" },
};
