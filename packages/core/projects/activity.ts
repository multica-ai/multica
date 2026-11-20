import type { Project } from "../types";

/** When the project or any of its issues last changed; older servers only know the project's own edits. */
export function projectLastActivityAt(project: Pick<Project, "last_activity_at" | "updated_at">): string {
  return project.last_activity_at || project.updated_at;
}
