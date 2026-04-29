import type { ProjectStatus } from "@/shared/types";

export const PROJECT_STATUS_ORDER: ProjectStatus[] = [
  "planned",
  "in_progress",
  "paused",
  "completed",
  "cancelled",
];

export const PROJECT_STATUS_CONFIG: Record<
  ProjectStatus,
  { label: string; badgeBg: string; badgeText: string; color: string }
> = {
  planned: {
    label: "Planned",
    badgeBg: "bg-muted",
    badgeText: "text-muted-foreground",
    color: "text-muted-foreground",
  },
  in_progress: {
    label: "In Progress",
    badgeBg: "bg-warning",
    badgeText: "text-white",
    color: "text-warning",
  },
  paused: {
    label: "Paused",
    badgeBg: "bg-secondary",
    badgeText: "text-secondary-foreground",
    color: "text-muted-foreground",
  },
  completed: {
    label: "Completed",
    badgeBg: "bg-success",
    badgeText: "text-white",
    color: "text-success",
  },
  cancelled: {
    label: "Cancelled",
    badgeBg: "bg-muted",
    badgeText: "text-muted-foreground",
    color: "text-muted-foreground",
  },
};