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
  urgent: { label: "Urgent", bars: 4, color: "text-priority-urgent", badgeBg: "bg-priority-urgent/10", badgeText: "text-priority-urgent" },
  high: { label: "High", bars: 3, color: "text-priority-high", badgeBg: "bg-priority-high/10", badgeText: "text-priority-high" },
  medium: { label: "Medium", bars: 2, color: "text-priority-medium", badgeBg: "bg-priority-medium/10", badgeText: "text-priority-medium" },
  low: { label: "Low", bars: 1, color: "text-priority-low", badgeBg: "bg-priority-low/10", badgeText: "text-priority-low" },
  none: { label: "No priority", bars: 0, color: "text-priority-none", badgeBg: "bg-muted", badgeText: "text-priority-none" },
};
