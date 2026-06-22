import type { IssueAssigneeGroup } from "@multica/core/types";
import type { IssueDateFilter } from "@multica/core/issues/stores/view-store";
import { filterIssues } from "../../issues/utils/filter";

export function filterRunningAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  agentRunningFilter: boolean,
  runningIssueIds: Set<string>,
): IssueAssigneeGroup[] | undefined {
  if (!groups || !agentRunningFilter) return groups;

  return groups
    .map((group) => {
      const issues = group.issues.filter((issue) => runningIssueIds.has(issue.id));
      return {
        ...group,
        issues,
        total: issues.length,
      };
    })
    .filter((group) => group.issues.length > 0);
}

// CEREBRO-PATCH(boards-date-filter): FIR-1871 apply stacked date filters to
// project/sprint assignee-board groups so grouped boards match list filtering.
export function filterDateAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  dateFilters: IssueDateFilter[],
): IssueAssigneeGroup[] | undefined {
  if (!groups || dateFilters.length === 0) return groups;

  return groups
    .map((group) => {
      const issues = filterIssues(group.issues, {
        statusFilters: [],
        priorityFilters: [],
        assigneeFilters: [],
        includeNoAssignee: false,
        creatorFilters: [],
        projectFilters: [],
        includeNoProject: false,
        labelFilters: [],
        dateFilters,
      });
      return {
        ...group,
        issues,
        total: issues.length,
      };
    })
    .filter((group) => group.issues.length > 0);
}
