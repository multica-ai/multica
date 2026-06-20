import type { ActorFilterValue } from "@multica/core/issues/stores/view-store";
import type { IssueStatus, IssuePriority } from "@multica/core/types";

/**
 * FilterSnapshot is the persisted shape of a saved filter — exactly the
 * filter-relevant slice of the issue view-store. It deliberately omits view
 * mode, sorting, grouping and card properties: a saved filter restores WHICH
 * issues are shown, not how they are laid out.
 */
export interface FilterSnapshot {
  statusFilters: IssueStatus[];
  priorityFilters: IssuePriority[];
  assigneeFilters: ActorFilterValue[];
  includeNoAssignee: boolean;
  creatorFilters: ActorFilterValue[];
  projectFilters: string[];
  includeNoProject: boolean;
  labelFilters: string[];
  onBehalfOfFilters: string[];
  agentRunningFilter: boolean;
}

export interface SavedFilter {
  id: string;
  name: string;
  surface: string;
  filterState: FilterSnapshot;
  position: number;
  createdAt: string;
  updatedAt: string;
}

export interface CreateSavedFilterInput {
  name: string;
  surface?: string;
  filterState: FilterSnapshot;
  position?: number;
}

export interface UpdateSavedFilterInput {
  name?: string;
  filterState?: FilterSnapshot;
  position?: number;
}
