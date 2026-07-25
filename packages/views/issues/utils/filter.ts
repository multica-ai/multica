import type { Issue, IssueStatus, IssuePriority, IssueAssigneeGroup } from "@multica/core/types";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";
import type { IssueActivityState } from "../surface/activity";

export interface IssueFilters {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** Keeps an explicitly active assignee predicate distinct from the normal
   *  empty-array = no-filter state. When true with no selected assignees and
   *  includeNoAssignee=false, the predicate intentionally matches nothing. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  /** Custom-property filters: definition id → selected option ids (OR within
   *  a definition, AND across definitions; checkbox uses "true"/"false"). */
  propertyFilters?: Record<string, string[]>;
  // When `agentRunningFilter` is true, only keep issues whose id is in
  // `runningIssueIds`. The surface derives this set from the independent
  // `/api/working-agents` projection so filter.ts stays free of fetching.
  agentRunningFilter?: boolean;
  runningIssueIds?: ReadonlySet<string>;
  // "Show sub-issues" display toggle. When explicitly `false`, hide issues
  // that have a parent so only top-level issues remain. Undefined / true keeps
  // the default behaviour of showing everything, so existing callers that omit
  // it (and mobile's positional variant) are unaffected.
  showSubIssues?: boolean;
}

export interface IssueFilterState {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  /** See IssueFilters.assigneeFilterActive. */
  assigneeFilterActive?: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  propertyFilters?: Record<string, string[]>;
  workingOnly: boolean;
  /** See IssueFilters.showSubIssues — only an explicit `false` hides. */
  showSubIssues?: boolean;
}

export interface IssueFilterContext {
  activityByIssueId?: ReadonlyMap<string, IssueActivityState>;
  runningIssueIds?: ReadonlySet<string>;
}

/**
 * Match one issue against the property filters. Select values are single
 * option-id strings, multi_select values are option-id arrays, checkbox
 * values are booleans compared against the "true"/"false" pseudo-options.
 * An issue with no value for a filtered definition never matches it.
 */
export function issueMatchesPropertyFilters(
  issue: Issue,
  propertyFilters: Record<string, string[]> | undefined,
): boolean {
  if (!propertyFilters) return true;
  for (const [propertyId, selected] of Object.entries(propertyFilters)) {
    if (selected.length === 0) continue;
    const value = issue.properties?.[propertyId];
    if (value === undefined) return false;
    if (typeof value === "string") {
      if (!selected.includes(value)) return false;
    } else if (Array.isArray(value)) {
      if (!value.some((id) => selected.includes(id))) return false;
    } else if (typeof value === "boolean") {
      if (!selected.includes(String(value))) return false;
    } else {
      return false;
    }
  }
  return true;
}

function issueIsWorking(issueId: string, context: IssueFilterContext) {
  if (context.activityByIssueId) {
    return context.activityByIssueId.get(issueId)?.isWorking === true;
  }
  return context.runningIssueIds?.has(issueId) === true;
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
export function applyIssueFilters(
  issues: Issue[],
  filters: IssueFilterState,
  context: IssueFilterContext = {},
): Issue[] {
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, workingOnly } = filters;
  const hasAssigneeFilter =
    filters.assigneeFilterActive === true ||
    assigneeFilters.length > 0 ||
    includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  // Empty set passed without `agentRunningFilter` is a no-op. When the
  // filter is on but the set is missing/empty, hide everything — the
  // user opted into "only running" and there is nothing running.
  const applyWorkingOnly = workingOnly === true;
  const hideSubIssues = filters.showSubIssues === false;

  return issues.filter((issue) => {
    if (applyWorkingOnly && !issueIsWorking(issue.id, context))
      return false;

    if (hideSubIssues && issue.parent_issue_id) return false;

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

    if (!issueMatchesPropertyFilters(issue, filters.propertyFilters)) return false;

    return true;
  });
}

export function filterIssues(issues: Issue[], filters: IssueFilters): Issue[] {
  return applyIssueFilters(
    issues,
    {
      statusFilters: filters.statusFilters,
      priorityFilters: filters.priorityFilters,
      assigneeFilters: filters.assigneeFilters,
      includeNoAssignee: filters.includeNoAssignee,
      assigneeFilterActive: filters.assigneeFilterActive,
      creatorFilters: filters.creatorFilters,
      projectFilters: filters.projectFilters,
      includeNoProject: filters.includeNoProject,
      labelFilters: filters.labelFilters,
      propertyFilters: filters.propertyFilters,
      workingOnly: filters.agentRunningFilter === true,
      showSubIssues: filters.showSubIssues,
    },
    { runningIssueIds: filters.runningIssueIds },
  );
}

/**
 * Re-apply the client-only display filters to a server-grouped response.
 * The assignee-grouped board renders straight from `groups`, bypassing the
 * flat `applyIssueFilters` output, so the "Show sub-issues" toggle and the
 * agents-working quick filter must be applied per group here. Recomputes
 * each group's total and drops emptied groups. Returns the input by
 * reference when no client filter is active.
 */
export function filterAssigneeGroups(
  groups: IssueAssigneeGroup[] | undefined,
  filters: {
    showSubIssues?: boolean;
    agentRunningFilter?: boolean;
    runningIssueIds?: ReadonlySet<string>;
    propertyFilters?: Record<string, string[]>;
  },
): IssueAssigneeGroup[] | undefined {
  const applyRunning = filters.agentRunningFilter === true;
  const hideSubIssues = filters.showSubIssues === false;
  const hasPropertyFilters = Object.values(filters.propertyFilters ?? {}).some(
    (selected) => selected.length > 0,
  );
  if (!groups || (!applyRunning && !hideSubIssues && !hasPropertyFilters)) return groups;

  const { runningIssueIds } = filters;
  return groups
    .map((group) => {
      const issues = group.issues.filter((issue) => {
        if (applyRunning && !(runningIssueIds?.has(issue.id) ?? false))
          return false;
        if (hideSubIssues && issue.parent_issue_id) return false;
        if (hasPropertyFilters && !issueMatchesPropertyFilters(issue, filters.propertyFilters))
          return false;
        return true;
      });
      return { ...group, issues, total: issues.length };
    })
    .filter((group) => group.issues.length > 0);
}
