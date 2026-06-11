import type { IssueAssigneeGroup } from "@multica/core/types";

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
