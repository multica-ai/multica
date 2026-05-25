import type { IssueListFilter } from "@multica/core/issues/queries";
import type { IssueStatus } from "@multica/core/types";
import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";

type IssueListViewFilters = {
  statusFilters?: readonly IssueStatus[];
  priorityFilters?: IssueListFilter["priorities"];
  assigneeFilters?: readonly ActorFilterValue[];
  includeNoAssignee?: boolean;
  creatorFilters?: readonly ActorFilterValue[];
  projectFilters?: readonly string[];
  includeNoProject?: boolean;
  labelFilters?: readonly string[];
};

export function buildIssueListServerFilter(
  base: IssueListFilter,
  filters: IssueListViewFilters,
  visibleStatuses: readonly IssueStatus[],
): IssueListFilter {
  const next: IssueListFilter = {
    ...base,
    statuses: visibleStatuses,
  };

  if (filters.priorityFilters?.length) {
    next.priorities = [...filters.priorityFilters];
  }
  if (filters.assigneeFilters?.length) {
    next.assignees = filters.assigneeFilters.map((filter) => ({
      type: filter.type,
      id: filter.id,
    }));
  }
  if (filters.includeNoAssignee) {
    next.include_no_assignee = true;
  }
  if (filters.creatorFilters?.length) {
    next.creators = filters.creatorFilters.map((filter) => ({
      type: filter.type,
      id: filter.id,
    }));
  }
  if (filters.projectFilters?.length) {
    next.project_ids = [...filters.projectFilters];
  }
  if (filters.includeNoProject) {
    next.include_no_project = true;
  }
  if (filters.labelFilters?.length) {
    next.label_ids = [...filters.labelFilters];
  }

  return next;
}
