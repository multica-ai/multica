import type { IssueAssigneeGroup } from "@multica/core/types";
import { filterIssues, type IssueFilters } from "./filter";

export function filterAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  filters: IssueFilters,
): IssueAssigneeGroup[] | undefined {
  if (!groups) return groups;
  const hasClientSideFilter = filters.agentRunningFilter === true;
  if (!hasClientSideFilter) return groups;

  return groups
    .map((group) => {
      const issues = filterIssues(group.issues, filters);
      return {
        ...group,
        issues,
        total: issues.length,
      };
    })
    .filter((group) => group.issues.length > 0);
}
