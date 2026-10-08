import type { IssuePriority } from "../../types";

/**
 * Severity order, high to low. This is the **sort rank** — `sortIssues` turns
 * the array index into the comparison weight — so "none" must stay last.
 * Never reorder this to change how a menu reads; use
 * {@link PRIORITY_DISPLAY_ORDER} for that.
 */
export const PRIORITY_ORDER: IssuePriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

/**
 * Order every user-facing priority list renders in: the empty value first,
 * then severity descending. Leading with "No priority" follows the convention
 * every other picker in the app uses — the first row is always the empty
 * value (unassigned / no project / no stage), so the eye finds "clear this
 * field" in the same place regardless of which pill was opened.
 */
export const PRIORITY_DISPLAY_ORDER: IssuePriority[] = [
  "none",
  "urgent",
  "high",
  "medium",
  "low",
];

export const PRIORITY_CONFIG: Record<
  IssuePriority,
  { label: string; bars: number; color: string; badgeBg: string; badgeText: string }
> = {
  urgent: { label: "Urgent", bars: 4, color: "text-destructive", badgeBg: "bg-destructive/10", badgeText: "text-destructive" },
  high: { label: "High", bars: 3, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
  medium: { label: "Medium", bars: 2, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
  low: { label: "Low", bars: 1, color: "text-info", badgeBg: "bg-info/10", badgeText: "text-info" },
  none: { label: "No priority", bars: 0, color: "text-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};
