import type { IssuePriority } from "@multica/core/types";
import type { IssueListFilter } from "@multica/core/issues/queries";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";

/**
 * Shape of the filter state held in the view store. Status is excluded — it
 * controls which buckets are visible on the page, not which buckets are
 * fetched (see {@link buildIssueListFilter} below).
 */
export interface IssueViewFilters {
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
}

/**
 * Translate the view-store filter state into the wire-shape filter accepted
 * by `GET /api/issues`. Each filter type is OR'd within itself and AND'd
 * with the others — same semantics the SQL layer enforces.
 *
 * Assignee and project filters can mix actor IDs with the "no assignee /
 * project" toggle. The toggle is encoded as `include_no_*: true`, which the
 * backend OR's against the id-list match.
 *
 * Returns an empty object when nothing is selected — callers can pass this
 * to {@link issueListOptions} as a no-op filter.
 */
export function buildIssueListFilter(
  filters: IssueViewFilters,
): IssueListFilter {
  const out: IssueListFilter = {};
  if (filters.priorityFilters.length > 0) {
    out.priorities = [...filters.priorityFilters];
  }
  // Assignee + creator filters carry an actor type alongside the id, but the
  // backend keys assignment by id alone (member/agent ids never collide), so
  // we send only the ids.
  if (filters.assigneeFilters.length > 0) {
    out.assignee_ids = filters.assigneeFilters.map((f) => f.id);
  }
  if (filters.includeNoAssignee) out.include_no_assignee = true;
  if (filters.creatorFilters.length > 0) {
    out.creator_ids = filters.creatorFilters.map((f) => f.id);
  }
  if (filters.projectFilters.length > 0) {
    out.project_ids = [...filters.projectFilters];
  }
  if (filters.includeNoProject) out.include_no_project = true;
  if (filters.labelFilters.length > 0) {
    out.label_ids = [...filters.labelFilters];
  }
  return out;
}
