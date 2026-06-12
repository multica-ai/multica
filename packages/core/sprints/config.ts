import type { SprintStatus } from "../types";

export const SPRINT_STATUS_CONFIG: Record<
  SprintStatus,
  { label: string; color: string; dotColor: string; badgeBg: string; badgeText: string }
> = {
  planned: { label: "Planned", color: "text-muted-foreground", dotColor: "bg-muted-foreground", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
  active: { label: "Active", color: "text-primary", dotColor: "bg-primary", badgeBg: "bg-primary", badgeText: "text-primary-foreground" },
  completed: { label: "Completed", color: "text-info", dotColor: "bg-info", badgeBg: "bg-info", badgeText: "text-white" },
  cancelled: { label: "Cancelled", color: "text-destructive", dotColor: "bg-destructive", badgeBg: "bg-muted", badgeText: "text-muted-foreground" },
};

export const SPRINT_STATUS_ORDER: SprintStatus[] = ["planned", "active", "completed", "cancelled"];
