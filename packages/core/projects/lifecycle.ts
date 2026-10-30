import type { ProjectStatus } from "../types";

export const PROJECT_LIFECYCLE_VIEWS = ["active", "completed", "all"] as const;
export type ProjectLifecycleView = (typeof PROJECT_LIFECYCLE_VIEWS)[number];

export function matchesProjectLifecycle(status: ProjectStatus, view: ProjectLifecycleView): boolean {
  if (view === "all") return true;
  if (view === "completed") return status === "completed";
  return status === "planned" || status === "in_progress" || status === "paused";
}
