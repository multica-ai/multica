import type { EpicStatus } from "../types";

export const EPIC_STATUS_CONFIG: Record<
  EpicStatus,
  { label: string; color: string; dotColor: string }
> = {
  open: { label: "Open", color: "text-primary", dotColor: "bg-primary" },
  closed: { label: "Closed", color: "text-muted-foreground", dotColor: "bg-muted-foreground" },
};

export const DEFAULT_EPIC_COLORS = [
  "#6366f1", "#8b5cf6", "#ec4899", "#f43f5e",
  "#f97316", "#eab308", "#22c55e", "#06b6d4",
];
