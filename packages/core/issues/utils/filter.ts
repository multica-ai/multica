import type { Issue, IssuePriority, IssueStatus } from "../../types";

export interface ActorFilterValue {
  type: "member" | "agent" | "squad";
  id: string;
}

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
}

/**
 * Filter issues using a positive selection model.
 * Empty arrays = no filter. Non-empty = show only matching.
 */
export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  const {
    statusFilters,
    priorityFilters,
    assigneeFilters,
    includeNoAssignee,
    creatorFilters,
    projectFilters,
    includeNoProject,
    labelFilters,
  } = filters;
  const hasAssigneeFilter = assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;

  return issues.filter((issue) => {
    if (statusFilters.length > 0 && !statusFilters.includes(issue.status))
      return false;

    if (priorityFilters.length > 0 && !priorityFilters.includes(issue.priority))
      return false;

    if (hasAssigneeFilter) {
      if (!issue.assignee_id) {
        if (!includeNoAssignee) return false;
      } else if (assigneeFilters.length > 0) {
        if (!assigneeFilters.some(
          (f) => f.type === issue.assignee_type && f.id === issue.assignee_id,
        )) return false;
      } else {
        return false;
      }
    }

    if (
      creatorFilters.length > 0 &&
      !creatorFilters.some(
        (f) => f.type === issue.creator_type && f.id === issue.creator_id,
      )
    ) {
      return false;
    }

    if (hasProjectFilter) {
      if (!issue.project_id) {
        if (!includeNoProject) return false;
      } else if (projectFilters.length > 0) {
        if (!projectFilters.includes(issue.project_id)) return false;
      } else {
        return false;
      }
    }

    if (labelFilters.length > 0) {
      const issueLabels = issue.labels?.map((l) => l.id) ?? [];
      if (!labelFilters.some((id) => issueLabels.includes(id))) return false;
    }

    return true;
  });
}
