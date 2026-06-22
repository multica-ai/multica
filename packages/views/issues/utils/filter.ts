import type { Issue, IssueStatus, IssuePriority } from "@multica/core/types";
import type { ActorFilterValue, IssueDateFilter } from "@multica/core/issues/stores/view-store";
// CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — client-side date matching for My Issues.
import { dateOnlyToUTCDate } from "@multica/core/issues/date";
// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — dynamic window resolver so
// presets and relative ranges re-derive from "today" instead of frozen from/to.
import { resolveDateWindow } from "./cerebro-date-presets";

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
  // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — client-side date
  // filter for My Issues. The global /issues page applies the same
  // IssueDateFilter server-side; My Issues fetches by scope and filters in the
  // client, so the range (or the "no due date" mode) is matched here.
  dateFilter?: IssueDateFilter | null;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date conditions
  // for the My Issues filter builder, combined with AND. Each entry is matched
  // independently against its own field; an empty/omitted array is a no-op.
  dateFilters?: IssueDateFilter[];
}

// CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — match an issue against
// an IssueDateFilter. `mode: "none"` keeps only issues with no due date;
// otherwise the chosen field's calendar day must fall within [from, to]
// inclusive (server uses >= from AND < to+1day — the same closed range).
//
// Note on created_at/updated_at: those carry a full ISO timestamp, while
// due_date is a bare "YYYY-MM-DD". dateOnlyToUTCDate() reads the leading
// calendar day from either form (see date.ts parseParts), so all three fields
// reduce to a UTC-midnight calendar day before the range compare — no
// time-of-day drift, and no field is silently excluded.
function matchesDateFilter(issue: Issue, filter: IssueDateFilter): boolean {
  const raw =
    filter.field === "due_date"
      ? issue.due_date
      : filter.field === "created_at"
        ? issue.created_at
        : issue.updated_at;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1812 — field-aware set-modes.
  // "none" = Is not set (v1 only ever used field=due_date, so !raw === the old
  // !issue.due_date); "set" = Is set / Any date.
  if (filter.mode === "none") return !raw;
  if (filter.mode === "set") return !!raw;
  if (!raw) return false;
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — re-derive the window now.
  const win = resolveDateWindow(filter);
  const day = dateOnlyToUTCDate(raw);
  const from = dateOnlyToUTCDate(win.from);
  const to = dateOnlyToUTCDate(win.to);
  if (!day || !from || !to) return false;
  const inWindow = day.getTime() >= from.getTime() && day.getTime() <= to.getTime();
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1812 — operator "Is not".
  return filter.negate ? !inWindow : inWindow;
}

// CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — every stacked condition
// must match (AND). An empty list is a no-op (keeps the issue).
function matchesDateFilters(issue: Issue, filters: IssueDateFilter[]): boolean {
  return filters.every((f) => matchesDateFilter(issue, f));
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
  const { statusFilters, priorityFilters, assigneeFilters, includeNoAssignee, creatorFilters, projectFilters, includeNoProject, labelFilters, agentRunningFilter, runningIssueIds, dateFilter, dateFilters } = filters;
  const hasAssigneeFilter = assigneeFilters.length > 0 || includeNoAssignee;
  const hasProjectFilter = projectFilters.length > 0 || includeNoProject;
  // Empty set passed without `agentRunningFilter` is a no-op. When the
  // filter is on but the set is missing/empty, hide everything — the
  // user opted into "only running" and there is nothing running.
  const applyAgentRunning = agentRunningFilter === true;

  return issues.filter((issue) => {
    if (applyAgentRunning && !(runningIssueIds?.has(issue.id) ?? false))
      return false;

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

    // CEREBRO-PATCH(my-issues-due-date-presets): FIR-1658 — date / due-date filter.
    if (dateFilter && !matchesDateFilter(issue, dateFilter)) return false;

    // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date conditions (AND).
    if (dateFilters && dateFilters.length > 0 && !matchesDateFilters(issue, dateFilters))
      return false;

    return true;
  });
}
