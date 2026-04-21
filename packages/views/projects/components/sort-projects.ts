import {
  PROJECT_PRIORITY_ORDER,
  PROJECT_STATUS_ORDER,
} from "@multica/core/projects/config";
import type {
  Project,
  ProjectPriority,
  ProjectStatus,
} from "@multica/core/types";
import type {
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects/stores";

const PRIORITY_RANK: Record<ProjectPriority, number> = Object.fromEntries(
  PROJECT_PRIORITY_ORDER.map((priority, index) => [priority, index]),
) as Record<ProjectPriority, number>;

const STATUS_RANK: Record<ProjectStatus, number> = Object.fromEntries(
  PROJECT_STATUS_ORDER.map((status, index) => [status, index]),
) as Record<ProjectStatus, number>;

function compareValues(
  a: Project,
  b: Project,
  field: ProjectSortField,
  direction: ProjectSortDirection,
): number {
  const directionFactor = direction === "desc" ? -1 : 1;

  switch (field) {
    case "priority":
      return (PRIORITY_RANK[a.priority] - PRIORITY_RANK[b.priority]) * directionFactor;
    case "status":
      return (STATUS_RANK[a.status] - STATUS_RANK[b.status]) * directionFactor;
    case "created_at":
      return (
        new Date(a.created_at).getTime() - new Date(b.created_at).getTime()
      ) * directionFactor;
    case "updated_at":
      return (
        new Date(a.updated_at).getTime() - new Date(b.updated_at).getTime()
      ) * directionFactor;
    case "title":
      return a.title.localeCompare(b.title) * directionFactor;
    default:
      return 0;
  }
}

export function sortProjects(
  projects: Project[],
  field: ProjectSortField,
  direction: ProjectSortDirection,
): Project[] {
  const originalOrder = new Map(
    projects.map((project, index) => [project.id, index] as const),
  );

  const sorted = [...projects].sort((a, b) => {
    const primary = compareValues(a, b, field, direction);
    if (primary !== 0) return primary;

    return (originalOrder.get(a.id) ?? 0) - (originalOrder.get(b.id) ?? 0);
  });

  return sorted;
}
