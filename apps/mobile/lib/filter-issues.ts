/**
 * Mirrors the status+priority slice of `filterIssues()` at
 * packages/views/issues/utils/filter.ts:30-34. Same predicate, same
 * "empty array = show all" semantics — required by the same-N parity rule
 * in apps/mobile/CLAUDE.md.
 *
 * Mobile only filters on status + priority for now; assignee / project /
 * label slots from the web filter are deferred to v2.
 */
import type { Issue, IssuePriority, IssueStatus } from "@multica/core/types";
import { CLOSED_CATEGORIES, issueBehavesAsAny } from "./issue-status";

export function filterIssues(
  issues: Issue[],
  statusFilters: IssueStatus[],
  priorityFilters: IssuePriority[],
): Issue[] {
  return issues.filter((issue) => {
    if (
      statusFilters.length > 0 &&
      !statusFilters.includes(issue.status)
    ) {
      return false;
    }
    if (
      priorityFilters.length > 0 &&
      !priorityFilters.includes(issue.priority)
    ) {
      return false;
    }
    return true;
  });
}

/**
 * The list with "Show completed" off: drops issues in the done/closed
 * categories, built-in or custom, by the same category rule as web's gantt
 * toggle (`ganttCanvasRows` in packages/views/issues/surface/
 * use-issue-surface-data.ts). A status selected in the status filter stays
 * visible — an explicit choice outranks the default. An unresolved custom
 * status stays visible too, so open work is never hidden by mistake.
 */
export function hideCompletedIssues(
  issues: Issue[],
  statusFilters: IssueStatus[],
): Issue[] {
  return issues.filter(
    (issue) =>
      statusFilters.includes(issue.status) ||
      !issueBehavesAsAny(issue, CLOSED_CATEGORIES),
  );
}
