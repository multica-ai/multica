import type { ProjectStatus } from "@/shared/types";

export const PROJECT_STATUSES: ProjectStatus[] = [
  "backlog",
  "planned",
  "in_progress",
  "completed",
  "cancelled",
];

export const PROJECT_STATUS_CONFIG: Record<
  ProjectStatus,
  { label: string; color: string; bg: string }
> = {
  backlog: { label: "Backlog", color: "text-muted-foreground", bg: "bg-muted" },
  planned: { label: "Planned", color: "text-blue-600", bg: "bg-blue-50 dark:bg-blue-950" },
  in_progress: { label: "In Progress", color: "text-yellow-600", bg: "bg-yellow-50 dark:bg-yellow-950" },
  completed: { label: "Completed", color: "text-green-600", bg: "bg-green-50 dark:bg-green-950" },
  cancelled: { label: "Cancelled", color: "text-muted-foreground", bg: "bg-muted" },
};

export const PROJECT_COLORS = [
  "#6366f1",
  "#8b5cf6",
  "#ec4899",
  "#ef4444",
  "#f97316",
  "#eab308",
  "#22c55e",
  "#14b8a6",
  "#06b6d4",
  "#3b82f6",
];
