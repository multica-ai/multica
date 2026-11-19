import type { ProjectGroupBy } from "@multica/core/projects";
import type { Project, UpdateProjectRequest } from "@multica/core/types";

export type ProjectGroupField = Exclude<ProjectGroupBy, "none">;

/** Group key of a project without a lead. Lead keys are otherwise "type:id". */
export const NO_LEAD_KEY = "none";
/** Group key of a project outside every folder. Folder keys are otherwise the folder id. */
export const NO_FOLDER_KEY = "none";

const NOT_FILED: ReadonlyMap<string, string> = new Map();

export interface ProjectGroupBucket {
  key: string;
  projects: Project[];
}

/** `folderOf` is where each project is filed, as the folders extension reports it. */
export function projectGroupKey(
  project: Project,
  field: ProjectGroupField,
  folderOf: ReadonlyMap<string, string> = NOT_FILED,
): string {
  if (field === "status") return project.status;
  if (field === "priority") return project.priority;
  if (field === "folder") return folderOf.get(project.id) ?? NO_FOLDER_KEY;
  return project.lead_type && project.lead_id
    ? `${project.lead_type}:${project.lead_id}`
    : NO_LEAD_KEY;
}

/** The change that moves a project into the group `key`. Folders move through their extension. */
export function projectGroupUpdate(field: Exclude<ProjectGroupField, "folder">, key: string): UpdateProjectRequest {
  if (field === "status") return { status: key as Project["status"] };
  if (field === "priority") return { priority: key as Project["priority"] };
  if (key === NO_LEAD_KEY) return { lead_type: null, lead_id: null };
  const [type, id] = key.split(":") as [Project["lead_type"], string];
  return { lead_type: type, lead_id: id };
}

/**
 * Buckets `projects` (already sorted) into `keys`, in that order, keeping each
 * bucket's order. Projects whose key is not listed are dropped, except that a
 * project whose folder is gone counts as outside every folder.
 */
export function groupProjects(
  projects: readonly Project[],
  field: ProjectGroupField,
  keys: readonly string[],
  folderOf: ReadonlyMap<string, string> = NOT_FILED,
): ProjectGroupBucket[] {
  const buckets = new Map<string, Project[]>(keys.map((key) => [key, []]));
  for (const project of projects) {
    const bucket =
      buckets.get(projectGroupKey(project, field, folderOf)) ??
      (field === "folder" ? buckets.get(NO_FOLDER_KEY) : undefined);
    bucket?.push(project);
  }
  return keys.map((key) => ({ key, projects: buckets.get(key) ?? [] }));
}
