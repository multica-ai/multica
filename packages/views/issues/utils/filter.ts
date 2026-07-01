import type { Issue, IssueAssigneeGroup, IssueStatus, IssuePriority } from "@multica/core/types";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  // When `agentRunningFilter` is true, only keep issues whose id is in
  // `runningIssueIds`. The set is derived by the caller from
  // `agentTaskSnapshot` (one pass over running tasks) so filter.ts stays
  // free of any data-fetching dependency.
  agentRunningFilter?: boolean;
  runningIssueIds?: ReadonlySet<string>;
  // "Show sub-issues" display toggle. When explicitly `false`, hide issues
  // that have a parent so only top-level issues remain. Undefined / true keeps
  // the default behaviour of showing everything, so existing callers that omit
  // it (and mobile's positional variant) are unaffected.
  showSubIssues?: boolean;
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
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds, showSubIssues } = filters;
  const hasAssigneeFilter = assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  // Empty set passed without `agentRunningFilter` is a no-op. When the
  // filter is on but the set is missing/empty, hide everything — the
  // user opted into "only running" and there is nothing running.
  const applyAgentRunning = agentRunningFilter === true;

  return issues.filter((issue) => {
    if (applyAgentRunning && !(runningIssueIds?.has(issue.id) ?? false))
      return false;

    // "Show sub-issues" off → keep only top-level issues.
    if (showSubIssues === false && issue.parent_issue_id) return false;

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

    if (labelFilters.length > 0) {
      // OR semantics within the filter: keep issues that carry any of the
      // selected labels. Matches existing priority / project multi-select.
      const issueLabels = issue.labels;
      if (!issueLabels || issueLabels.length === 0) return false;
      if (!issueLabels.some((l) => labelFilters.includes(l.id))) return false;
    }

    return true;
  });
}

/**
 * Apply the client-only display filters to assignee-grouped board data.
 *
 * The assignee-grouped board renders from the server-provided `groups` array
 * rather than the flat `filterIssues` output, so display toggles that live
 * purely on the client — "Show sub-issues" and the "agents working" quick
 * filter — must be re-applied here or they silently bypass the assignee board.
 * Only these two are handled: everything else (status, priority, label, …) is
 * already enforced server-side when the groups are fetched, and re-running it
 * here could double-filter against stale query params.
 *
 * Filtered groups get their `total` recomputed and emptied groups dropped. When
 * neither toggle is active the original array is returned by reference so
 * callers don't churn renders.
 */
export function filterAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  filters: {
    showSubIssues?: boolean;
    agentRunningFilter?: boolean;
    runningIssueIds?: ReadonlySet<string>;
  },
): IssueAssigneeGroup[] | undefined {
  const applyRunning = filters.agentRunningFilter === true;
  const hideSubIssues = filters.showSubIssues === false;
  if (!groups || (!applyRunning && !hideSubIssues)) return groups;

  const { runningIssueIds } = filters;
  return groups
    .map((group) => {
      const issues = group.issues.filter((issue) => {
        if (applyRunning && !(runningIssueIds?.has(issue.id) ?? false))
          return false;
        if (hideSubIssues && issue.parent_issue_id) return false;
        return true;
      });
      return { ...group, issues, total: issues.length };
    })
    .filter((group) => group.issues.length > 0);
}
