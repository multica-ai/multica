import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";
import type {
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectPriority, ProjectStatus, ProjectTreeItem } from "@multica/core/types";

export interface ProjectCardItem {
  project: ProjectTreeItem;
  path: string[];
  activeSprint: WorkspaceSprint | null;
}

export interface ProjectCardSection {
  project: ProjectTreeItem;
  cards: ProjectCardItem[];
  activeSprint: WorkspaceSprint | null;
}

export interface ProjectCardGroups {
  sections: ProjectCardSection[];
  otherProjects: ProjectCardItem[];
}

export interface BuildProjectCardGroupsInput {
  tree: ProjectTreeItem[];
  sprints: WorkspaceSprint[];
  visibleProjectIds?: readonly string[];
  search: string;
  filters: ProjectListFilters;
  sortField: ProjectSortField;
  sortDirection: ProjectSortDirection;
}

const PRIORITY_ORDER: Record<ProjectPriority, number> = {
  urgent: 4,
  high: 3,
  medium: 2,
  low: 1,
  none: 0,
};

const STATUS_ORDER: Record<ProjectStatus, number> = {
  planned: 0,
  in_progress: 1,
  paused: 2,
  completed: 3,
  cancelled: 4,
};

function leadValue(project: ProjectTreeItem): string | null {
  return project.lead_type && project.lead_id
    ? `${project.lead_type}:${project.lead_id}`
    : null;
}

function matchesProject(
  project: ProjectTreeItem,
  query: string,
  filters: ProjectListFilters,
): boolean {
  if (query && !project.title.toLocaleLowerCase().includes(query)) return false;
  if (filters.statuses.length > 0 && !filters.statuses.includes(project.status)) return false;
  if (filters.priorities.length > 0 && !filters.priorities.includes(project.priority)) {
    return false;
  }
  if (filters.leads.length > 0) {
    const lead = leadValue(project);
    if (!lead || !filters.leads.includes(lead)) return false;
  }
  return true;
}

function compareProjects(
  a: ProjectTreeItem,
  b: ProjectTreeItem,
  field: ProjectSortField,
  direction: ProjectSortDirection,
): number {
  const dir = direction === "asc" ? 1 : -1;
  let result = 0;
  if (field === "name") result = a.title.localeCompare(b.title);
  else if (field === "priority") result = PRIORITY_ORDER[a.priority] - PRIORITY_ORDER[b.priority];
  else if (field === "status") result = STATUS_ORDER[a.status] - STATUS_ORDER[b.status];
  else if (field === "progress") {
    const aProgress = a.issue_count > 0 ? a.done_count / a.issue_count : -1;
    const bProgress = b.issue_count > 0 ? b.done_count / b.issue_count : -1;
    result = aProgress - bProgress;
  } else result = Date.parse(a.created_at) - Date.parse(b.created_at);
  return result * dir || a.title.localeCompare(b.title);
}

function activeSprintFor(
  projectId: string,
  sprintsByProject: Map<string, WorkspaceSprint[]>,
): WorkspaceSprint | null {
  return (
    [...(sprintsByProject.get(projectId) ?? [])]
      .filter((sprint) => sprint.status === "active")
      .sort(
        (a, b) =>
          a.end_date.localeCompare(b.end_date) ||
          a.start_date.localeCompare(b.start_date) ||
          a.sequence_no - b.sequence_no,
      )[0] ?? null
  );
}

export function buildProjectCardGroups(input: BuildProjectCardGroupsInput): ProjectCardGroups {
  const query = input.search.trim().toLocaleLowerCase();
  const visibleProjectIds = input.visibleProjectIds
    ? new Set(input.visibleProjectIds)
    : null;
  const isVisible = (project: ProjectTreeItem) =>
    visibleProjectIds
      ? visibleProjectIds.has(project.id)
      : matchesProject(project, query, input.filters);
  const compare = (a: ProjectTreeItem, b: ProjectTreeItem) =>
    compareProjects(a, b, input.sortField, input.sortDirection);
  const sprintsByProject = new Map<string, WorkspaceSprint[]>();
  for (const sprint of input.sprints) {
    const projectSprints = sprintsByProject.get(sprint.project_id) ?? [];
    projectSprints.push(sprint);
    sprintsByProject.set(sprint.project_id, projectSprints);
  }

  const cardFor = (project: ProjectTreeItem, path: string[]): ProjectCardItem => ({
    project,
    path,
    activeSprint: activeSprintFor(project.id, sprintsByProject),
  });
  const collectDescendants = (
    projects: ProjectTreeItem[],
    path: string[],
  ): ProjectCardItem[] => {
    const cards: ProjectCardItem[] = [];
    for (const project of [...projects].sort(compare)) {
      if (isVisible(project)) cards.push(cardFor(project, path));
      cards.push(...collectDescendants(project.children ?? [], [...path, project.title]));
    }
    return cards;
  };

  const sections: ProjectCardSection[] = [];
  const otherProjects: ProjectCardItem[] = [];
  for (const project of [...input.tree].sort(compare)) {
    if ((project.children?.length ?? 0) > 0) {
      const cards = collectDescendants(project.children ?? [], []);
      if (isVisible(project) || cards.length > 0) {
        sections.push({
          project,
          cards,
          activeSprint: activeSprintFor(project.id, sprintsByProject),
        });
      }
    } else if (isVisible(project)) {
      otherProjects.push(cardFor(project, []));
    }
  }

  return { sections, otherProjects };
}
