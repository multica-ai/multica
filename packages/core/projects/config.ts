// CEREBRO-PATCH(core-projects-config): cerebro modification of upstream file
import type { ProjectStatus, ProjectPriority } from "../types";

export const PROJECT_COLORS: { value: string; label: string; bg: string; text: string; dot: string }[] = [
  { value: "gray", label: "Gray", bg: "bg-zinc-100 dark:bg-zinc-800", text: "text-zinc-700 dark:text-zinc-300", dot: "bg-zinc-500" },
  { value: "red", label: "Red", bg: "bg-red-100 dark:bg-red-950", text: "text-red-700 dark:text-red-300", dot: "bg-red-500" },
  { value: "orange", label: "Orange", bg: "bg-orange-100 dark:bg-orange-950", text: "text-orange-700 dark:text-orange-300", dot: "bg-orange-500" },
  { value: "amber", label: "Amber", bg: "bg-amber-100 dark:bg-amber-950", text: "text-amber-700 dark:text-amber-300", dot: "bg-amber-500" },
  { value: "green", label: "Green", bg: "bg-emerald-100 dark:bg-emerald-950", text: "text-emerald-700 dark:text-emerald-300", dot: "bg-emerald-500" },
  { value: "blue", label: "Blue", bg: "bg-blue-100 dark:bg-blue-950", text: "text-blue-700 dark:text-blue-300", dot: "bg-blue-500" },
  { value: "purple", label: "Purple", bg: "bg-purple-100 dark:bg-purple-950", text: "text-purple-700 dark:text-purple-300", dot: "bg-purple-500" },
  { value: "pink", label: "Pink", bg: "bg-pink-100 dark:bg-pink-950", text: "text-pink-700 dark:text-pink-300", dot: "bg-pink-500" },
];

const DEFAULT_PROJECT_COLOR = { value: "gray", label: "Gray", bg: "bg-zinc-100 dark:bg-zinc-800", text: "text-zinc-700 dark:text-zinc-300", dot: "bg-zinc-500" };

export function getProjectColor(color: string | null) {
  return PROJECT_COLORS.find((c) => c.value === color) ?? DEFAULT_PROJECT_COLOR;
}

export const PROJECT_STATUS_ORDER: ProjectStatus[] = [
  "planned",
  "in_progress",
  "paused",
  "completed",
  "cancelled",
];

export const PROJECT_STATUS_CONFIG: Record<
  ProjectStatus,
  { label: string; color: string; dotColor: string; badgeBg: string; badgeText: string }
> = {
  planned: { label: "Planned", color: "text-muted-foreground", dotColor: "bg-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  in_progress: { label: "In Progress", color: "text-warning", dotColor: "bg-warning", badgeBg: "bg-warning", badgeText: "text-white" },
  paused: { label: "Paused", color: "text-muted-foreground", dotColor: "bg-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  completed: { label: "Completed", color: "text-info", dotColor: "bg-info", badgeBg: "bg-info", badgeText: "text-white" },
  cancelled: { label: "Cancelled", color: "text-destructive", dotColor: "bg-destructive", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};

export const PROJECT_PRIORITY_ORDER: ProjectPriority[] = [
  "urgent",
  "high",
  "medium",
  "low",
  "none",
];

export const PROJECT_PRIORITY_CONFIG: Record<
  ProjectPriority,
  { label: string; bars: number; color: string; badgeBg: string; badgeText: string }
> = {
  urgent: { label: "Urgent", bars: 4, color: "text-destructive", badgeBg: "bg-destructive/10", badgeText: "text-destructive" },
  high: { label: "High", bars: 3, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
  medium: { label: "Medium", bars: 2, color: "text-warning", badgeBg: "bg-warning/10", badgeText: "text-warning" },
  low: { label: "Low", bars: 1, color: "text-info", badgeBg: "bg-info/10", badgeText: "text-info" },
  none: { label: "No priority", bars: 0, color: "text-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};
