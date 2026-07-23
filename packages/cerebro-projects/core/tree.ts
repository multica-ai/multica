import type { WorkspaceSprint } from "@multica/cerebro-sprints/core";
import type {
  ProjectListFilters,
  ProjectSortDirection,
  ProjectSortField,
} from "@multica/core/projects";
import type { ProjectPriority, ProjectStatus, ProjectTreeItem } from "@multica/core/types";

export type ProjectTreeRow =
  | {
      kind: "project";
      project: ProjectTreeItem;
      depth: number;
      path: string[];
      contextOnly: boolean;
      hasChildren: boolean;
      expanded: boolean;
    }
  | {
      kind: "sprint";
      sprint: WorkspaceSprint;
      projectId: string;
      depth: number;
    }
  | {
      kind: "completed-toggle";
      projectId: string;
      depth: number;
      count: number;
      expanded: boolean;
    };

export interface BuildProjectTreeRowsInput {
  tree: ProjectTreeItem[];
  sprints: WorkspaceSprint[];
  expandedProjects: Record<string, boolean>;
  showCompletedSprints: Record<string, boolean>;
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

function matchesFilters(project: ProjectTreeItem, filters: ProjectListFilters): boolean {
  if (filters.statuses.length > 0 && !filters.statuses.includes(project.status)) return false;
  if (filters.priorities.length > 0 && !filters.priorities.includes(project.priority)) return false;
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

function sortedSprints(sprints: WorkspaceSprint[]): WorkspaceSprint[] {
  const statusOrder: Record<WorkspaceSprint["status"], number> = {
    active: 0,
    planned: 1,
    done: 2,
    cancelled: 3,
  };
  return [...sprints].sort(
    (a, b) =>
      statusOrder[a.status] - statusOrder[b.status] ||
      a.start_date.localeCompare(b.start_date) ||
      a.sequence_no - b.sequence_no,
  );
}

export function buildProjectTreeRows(input: BuildProjectTreeRowsInput): ProjectTreeRow[] {
  const query = input.search.trim().toLocaleLowerCase();
  const hasFilters =
    input.filters.statuses.length > 0 ||
    input.filters.priorities.length > 0 ||
    input.filters.leads.length > 0;
  const compare = (a: ProjectTreeItem, b: ProjectTreeItem) =>
    compareProjects(a, b, input.sortField, input.sortDirection);

  if (query) {
    const matches: ProjectTreeRow[] = [];
    const collect = (projects: ProjectTreeItem[], path: string[]) => {
      for (const project of projects) {
        if (project.title.toLocaleLowerCase().includes(query) && matchesFilters(project, input.filters)) {
          matches.push({
            kind: "project",
            project,
            depth: 0,
            path,
            contextOnly: false,
            hasChildren: false,
            expanded: true,
          });
        }
        collect(project.children ?? [], [...path, project.title]);
      }
    };
    collect(input.tree, []);
    return matches.sort((a, b) =>
      a.kind === "project" && b.kind === "project" ? compare(a.project, b.project) : 0,
    );
  }

  const sprintsByProject = new Map<string, WorkspaceSprint[]>();
  for (const sprint of input.sprints) {
    const current = sprintsByProject.get(sprint.project_id) ?? [];
    current.push(sprint);
    sprintsByProject.set(sprint.project_id, current);
  }

  type VisibleNode = {
    project: ProjectTreeItem;
    selfMatches: boolean;
    children: VisibleNode[];
  };
  const selectVisible = (projects: ProjectTreeItem[]): VisibleNode[] =>
    projects.flatMap((project) => {
      const children = selectVisible(project.children ?? []);
      const selfMatches = matchesFilters(project, input.filters);
      return !hasFilters || selfMatches || children.length > 0
        ? [{ project, selfMatches, children }]
        : [];
    });

  const result: ProjectTreeRow[] = [];
  const append = (nodes: VisibleNode[], depth: number, path: string[]) => {
    for (const node of [...nodes].sort((a, b) => compare(a.project, b.project))) {
      const projectSprints = sortedSprints(sprintsByProject.get(node.project.id) ?? []);
      const openSprints = projectSprints.filter(
        (sprint) => sprint.status === "active" || sprint.status === "planned",
      );
      const doneSprints = projectSprints.filter((sprint) => sprint.status === "done");
      const hasChildren = node.children.length > 0 || openSprints.length > 0 || doneSprints.length > 0;
      const expanded = hasFilters || input.expandedProjects[node.project.id] !== false;
      result.push({
        kind: "project",
        project: node.project,
        depth,
        path,
        contextOnly: hasFilters && !node.selfMatches,
        hasChildren,
        expanded,
      });
      if (!expanded) continue;

      if (node.selfMatches) {
        for (const sprint of openSprints) {
          result.push({ kind: "sprint", sprint, projectId: node.project.id, depth: depth + 1 });
        }
        if (input.showCompletedSprints[node.project.id]) {
          for (const sprint of doneSprints) {
            result.push({ kind: "sprint", sprint, projectId: node.project.id, depth: depth + 1 });
          }
        }
        if (doneSprints.length > 0) {
          result.push({
            kind: "completed-toggle",
            projectId: node.project.id,
            depth: depth + 1,
            count: doneSprints.length,
            expanded: input.showCompletedSprints[node.project.id] === true,
          });
        }
      }
      append(node.children, depth + 1, [...path, node.project.title]);
    }
  };
  append(selectVisible(input.tree), 0, []);
  return result;
}
