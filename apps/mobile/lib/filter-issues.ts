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
