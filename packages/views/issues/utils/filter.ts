import type { Issue, IssueStatus, IssuePriority } from "@multica/core/types";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
}

/**
 * Filter issues using positive selection model.
 * Empty arrays = no filter (show all). Non-empty = show only matching.
 *
 * Assignee has a special "No assignee" toggle (includeNoAssignee):
 * - When only includeNoAssignee is true → show only unassigned issues
 * - When assigneeFilters has items → show only those assignees' issues
 * - When both → show matching assignees + unassigned
 */
export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject } = filters;
  const hasAssigneeFilter = assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;

  return issues.filter((issue) => {
    if (statusFilters.length > 0 && !statusFilters.includes(issue.status))
      return false;

    if (priorityFilters.length > 0 && !priorityFilters.includes(issue.priority))
      return false;

    if (hasAssigneeFilter) {
      if (!issue.assignee_id) {
        // Unassigned issue — show only if "No assignee" is checked
        if (!includeNoAssignee) return false;
      } else if (assigneeFilters.length > 0) {
        // Assigned issue — show only if assignee is in the filter list
        if (!assigneeFilters.some(
          (f) => f.type === issue.assignee_type && f.id === issue.assignee_id,
        )) return false;
      } else {
        // Only "No assignee" is checked, no specific assignees → hide assigned issues
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
        // Only "No project" is checked → hide issues that have a project
        return false;
      }
    }

    return true;
  });
}
