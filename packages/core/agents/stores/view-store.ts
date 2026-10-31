"use client";

import {
  createCollectionViewStore,
  type CollectionSortDirection,
  type CollectionViewCore,
} from "../../collections/view-store";
import type { AccessScope } from "../effective-access";

// View preferences for the agents list page: scope, sort, column visibility,
// and filters. Persisted per workspace, per user/device. Row selection is
// session-scoped on purpose (same rationale as the skills/autopilots view
// stores).

// Scope mixes the ownership lens (mine/all) with the archived lifecycle
// stage. Impure on paper, but the three are mutually exclusive in practice
// and "mine" is the historical product default; the archived view ignores
// the ownership lens entirely (showing only *my* archived agents would hide
// other people's archived agents with no UI to explain why).
export type AgentsScope = "mine" | "all" | "archived";

export const AGENT_SCOPES: AgentsScope[] = ["mine", "all", "archived"];

export type AgentSortField = "lastActive" | "name" | "runs" | "created";

export type AgentSortDirection = CollectionSortDirection;

/** Per-field direction applied when the user switches TO that field. */
export const AGENT_SORT_DEFAULT_DIRECTION: Record<
  AgentSortField,
  AgentSortDirection
> = {
  lastActive: "desc",
  name: "asc",
  runs: "desc",
  created: "desc",
};

/** Multi-select filter state. Empty array per dimension = inactive. */
export interface AgentListFilters {
  /** AgentAvailability values (online / unstable / offline). */
  availability: string[];
  /** Runtime ids. */
  runtimes: string[];
  /** Owner user ids. Owner is the same person-axis as the Mine scope: the
   *  "mine" scope is the clean no-filter personal view, and applying any
   *  filter (owner or otherwise) leaves it for "all" — see setScope /
   *  toggleFilter. So owner-as-filter and Mine never coexist, which keeps
   *  the axis orthogonal (no "mine + owner=someone-else = empty" state). */
  owners: string[];
  /** Runtime-native model identifiers (e.g. claude / codex / gpt-…). */
  models: string[];
  /** Effective access-scope values (MUL-3963): workspace | specific-people | owner-only. */
  access: AccessScope[];
}

export const EMPTY_AGENT_FILTERS: AgentListFilters = {
  availability: [],
  runtimes: [],
  owners: [],
  models: [],
  access: [],
};

// User-hideable columns. Name and the structural columns (checkbox, kebab)
// are always visible.
export type AgentColumnKey =
  | "status"
  | "owner"
  | "access"
  | "runtime"
  | "lastActive"
  | "runs"
  | "model"
  | "created";

/** Model stays on by default — comparing which model each agent runs is a
 *  core use of the list (GitHub #9500). Owner, access and created are opt-in
 *  so the default columns fit a 1280px window without horizontal scrolling;
 *  owner is also constant in the default "mine" scope. */
export const AGENT_DEFAULT_HIDDEN_COLUMNS: AgentColumnKey[] = [
  "owner",
  "access",
  "created",
];

/** Persist schema version. v0 hid the model column by default; v1 shows it;
 *  v2 also hides owner and access by default. */
const AGENT_VIEW_STORE_VERSION = 2;

export interface AgentsViewState
  extends CollectionViewCore<AgentSortField, AgentColumnKey, AgentListFilters> {
  scope: AgentsScope;
  setScope: (scope: AgentsScope) => void;
}

export const useAgentsViewStore = createCollectionViewStore<
  AgentSortField,
  AgentColumnKey,
  AgentListFilters,
  { scope: AgentsScope },
  { setScope: (scope: AgentsScope) => void }
>({
  name: "multica_agents_view",
  version: AGENT_VIEW_STORE_VERSION,
  // v0 → v1: model went from hidden-by-default to shown-by-default. Every v0
  // payload carried "model" in hiddenColumns unless the user had turned it
  // on, so un-hide it once; the user can re-hide it from the Display panel.
  // v1 → v2: a payload still on the v1 default (only "created" hidden) moves
  // to the new default; any other column choice is the user's and stays.
  // Other persisted prefs pass through untouched.
  migrate: (persisted, version) => {
    let p = (persisted ?? {}) as Partial<AgentsViewState>;
    if (version < 1 && Array.isArray(p.hiddenColumns)) {
      p = { ...p, hiddenColumns: p.hiddenColumns.filter((k) => k !== "model") };
    }
    if (
      version < 2 &&
      Array.isArray(p.hiddenColumns) &&
      p.hiddenColumns.length === 1 &&
      p.hiddenColumns[0] === "created"
    ) {
      p = { ...p, hiddenColumns: [...AGENT_DEFAULT_HIDDEN_COLUMNS] };
    }
    return p;
  },
  defaultSortField: "lastActive",
  sortDefaultDirection: AGENT_SORT_DEFAULT_DIRECTION,
  defaultHiddenColumns: AGENT_DEFAULT_HIDDEN_COLUMNS,
  emptyFilters: EMPTY_AGENT_FILTERS,
  // "mine" is the historical default — most members care about their own
  // agents first; admins flip to "all".
  extraState: { scope: "mine" },
  // "Mine" is the clean personal view: entering it clears all filters, so
  // Mine never carries filters. Switching to all/archived leaves filters
  // intact (you can carry "owner = Bob" between them).
  extraActions: (set) => ({
    setScope: (scope) =>
      set(
        scope === "mine" ? { scope, filters: EMPTY_AGENT_FILTERS } : { scope },
      ),
  }),
  // Applying any filter leaves the clean "mine" view for "all". Archived
  // keeps its own scope (it can carry filters).
  onToggleFilter: (state) => ({
    scope: state.scope === "mine" ? "all" : state.scope,
  }),
});
