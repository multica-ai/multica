import type { Issue } from "@multica/core/types";
import { issueColumnCategory } from "@multica/core/issues";

/**
 * The three lists an assignee's issues fall into. Cancelled issues belong to
 * none of them and are left out.
 */
export type CompanyIssueGroup = "active" | "planned" | "done";

export const COMPANY_ISSUE_GROUPS: readonly CompanyIssueGroup[] = ["active", "planned", "done"];

export function groupCompanyIssues(issues: readonly Issue[]): Record<CompanyIssueGroup, Issue[]> {
  const groups: Record<CompanyIssueGroup, Issue[]> = { active: [], planned: [], done: [] };
  for (const issue of issues) {
    const category = issueColumnCategory(issue);
    if (category === "started") groups.active.push(issue);
    else if (category === "unstarted") groups.planned.push(issue);
    else if (category === "done") groups.done.push(issue);
  }
  return groups;
}
