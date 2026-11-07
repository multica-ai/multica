import type { ProjectStatus } from "../types";

export const PROJECT_LIFECYCLE_VIEWS = ["active", "closed", "all"] as const;
export type ProjectLifecycleView = (typeof PROJECT_LIFECYCLE_VIEWS)[number];

export function matchesProjectLifecycle(status: ProjectStatus, view: ProjectLifecycleView): boolean {
  if (view === "all") return true;
  if (view === "closed") return status === "completed" || status === "cancelled";
  return status === "planned" || status === "in_progress" || status === "paused";
}
