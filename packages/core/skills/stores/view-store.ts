"use client";

import {
  createCollectionViewStore,
  type CollectionSortDirection,
  type CollectionViewCore,
} from "../../collections/view-store";

// View preferences for the skills list page: sort, column visibility, and
// filters. Persisted per workspace (workspace-aware storage), per user/device
// (localStorage). Search text and row selection are deliberately NOT stored —
// they are session-scoped, and persisting them would greet returning users
// with an inexplicably narrowed list.

export type SkillSortField = "name" | "usedBy" | "updated" | "created";

export type SkillSortDirection = CollectionSortDirection;

/** Per-field direction applied when the user switches TO that field. */
export const SKILL_SORT_DEFAULT_DIRECTION: Record<
  SkillSortField,
  SkillSortDirection
> = {
  name: "asc",
  usedBy: "desc",
  updated: "desc",
  created: "desc",
};

export type SkillOriginType =
  | "manual"
  | "runtime_local"
  | "clawhub"
  | "skills_sh"
  | "github";

/** Multi-select filter state. Empty array per dimension = inactive. */
export interface SkillListFilters {
  usage: ("used" | "unused")[];
  origins: SkillOriginType[];
  agents: string[];
  creators: string[];
  labels: string[];
}

export const EMPTY_SKILL_FILTERS: SkillListFilters = {
  usage: [],
  origins: [],
  agents: [],
  creators: [],
  labels: [],
};

// User-hideable columns. Name and the structural columns (checkbox, kebab)
// are always visible.
export type SkillColumnKey =
  | "usedBy"
  | "source"
  | "creator"
  | "updated"
  | "created";

/** Source and created are opt-in: hidden until the user enables them. */
export const DEFAULT_HIDDEN_COLUMNS: SkillColumnKey[] = ["source", "created"];

export type SkillsViewState = CollectionViewCore<
  SkillSortField,
  SkillColumnKey,
  SkillListFilters
>;

export const useSkillsViewStore = createCollectionViewStore<
  SkillSortField,
  SkillColumnKey,
  SkillListFilters
>({
  name: "multica_skills_view",
  defaultSortField: "updated",
  sortDefaultDirection: SKILL_SORT_DEFAULT_DIRECTION,
  defaultHiddenColumns: DEFAULT_HIDDEN_COLUMNS,
  emptyFilters: EMPTY_SKILL_FILTERS,
});
