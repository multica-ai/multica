import type { ProjectGroupBy } from "@multica/core/projects";
import type { Project, UpdateProjectRequest } from "@multica/core/types";

export type ProjectGroupField = Exclude<ProjectGroupBy, "none">;

/** Group key of a project without a lead. Lead keys are otherwise "type:id". */
export const NO_LEAD_KEY = "none";

export interface ProjectGroupBucket {
  key: string;
  projects: Project[];
}

export function projectGroupKey(project: Project, field: ProjectGroupField): string {
  if (field === "status") return project.status;
  if (field === "priority") return project.priority;
  return project.lead_type && project.lead_id
    ? `${project.lead_type}:${project.lead_id}`
    : NO_LEAD_KEY;
}

/** The change that moves a project into the group `key`. */
export function projectGroupUpdate(field: ProjectGroupField, key: string): UpdateProjectRequest {
  if (field === "status") return { status: key as Project["status"] };
  if (field === "priority") return { priority: key as Project["priority"] };
  if (key === NO_LEAD_KEY) return { lead_type: null, lead_id: null };
  const [type, id] = key.split(":") as [Project["lead_type"], string];
  return { lead_type: type, lead_id: id };
}

/**
 * Buckets `projects` (already sorted) into `keys`, in that order, keeping each
 * bucket's order. Projects whose key is not listed are dropped.
 */
export function groupProjects(
  projects: readonly Project[],
  field: ProjectGroupField,
  keys: readonly string[],
): ProjectGroupBucket[] {
  const buckets = new Map<string, Project[]>(keys.map((key) => [key, []]));
  for (const project of projects) buckets.get(projectGroupKey(project, field))?.push(project);
  return keys.map((key) => ({ key, projects: buckets.get(key) ?? [] }));
}
