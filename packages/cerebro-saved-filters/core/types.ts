import type { ActorFilterValue, IssueDateFilter } from "@multica/core/issues/stores/view-store";
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
  // CEREBRO-PATCH(my-issues-date-builder): FIR-1658 — stacked date conditions
  // (My Issues). Optional for backward-compat with presets saved before this.
  dateFilters?: IssueDateFilter[];
}

/**
 * Who may USE a saved filter (FIR-1659 Fase 3), mirroring the folder sharing
 * model the app already uses:
 *   private    — only the owner.
 *   shared     — the owner plus the explicit member/group `shares` targets.
 *   workspace  — every member of the workspace.
 */
export type SavedFilterVisibility = "private" | "shared" | "workspace";

/** One group or member a `shared` filter is shared with. */
export interface SavedFilterShareTarget {
  targetType: "group" | "member";
  targetId: string;
}

export interface SavedFilter {
  id: string;
  name: string;
  surface: string;
  filterState: FilterSnapshot;
  position: number;
  visibility: SavedFilterVisibility;
  /** The member who created the filter (their user id). */
  ownerId: string;
  /** Display name of the owner — present on filters shared by someone else. */
  ownerName?: string;
  /** True when the calling member owns the filter (may rename/share/delete). */
  isOwner: boolean;
  /** Share targets — only returned on the single-filter GET (owner-only). */
  shares?: SavedFilterShareTarget[];
  createdAt: string;
  updatedAt: string;
}

export interface CreateSavedFilterInput {
  name: string;
  surface?: string;
  filterState: FilterSnapshot;
  position?: number;
  visibility?: SavedFilterVisibility;
  shares?: SavedFilterShareTarget[];
}

export interface UpdateSavedFilterInput {
  name?: string;
  filterState?: FilterSnapshot;
  position?: number;
  visibility?: SavedFilterVisibility;
  shares?: SavedFilterShareTarget[];
}
