"use client";

import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import {
  createWorkspaceAwareStorage,
  registerForWorkspaceRehydration,
} from "../../platform/workspace-storage";
import { defaultStorage } from "../../platform/storage";
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

export type AgentSortDirection = "asc" | "desc";

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

/** Model and created are opt-in: hidden until the user enables them. Owner
 *  is shown by default (the user wants to see who owns each agent). */
export const AGENT_DEFAULT_HIDDEN_COLUMNS: AgentColumnKey[] = [
  "model",
  "created",
];

export interface AgentsViewState {
  scope: AgentsScope;
  sortField: AgentSortField;
  sortDirection: AgentSortDirection;
  hiddenColumns: AgentColumnKey[];
  filters: AgentListFilters;
  setScope: (scope: AgentsScope) => void;
  /** Header click: toggles direction on the active field, otherwise switches
   *  to the field with its default direction. */
  toggleSort: (field: AgentSortField) => void;
  /** Display panel select: switches field (default direction), no toggle. */
  setSortField: (field: AgentSortField) => void;
  setSortDirection: (direction: AgentSortDirection) => void;
  toggleColumn: (key: AgentColumnKey) => void;
  toggleFilter: (key: keyof AgentListFilters, value: string) => void;
  clearFilters: () => void;
}

const DEFAULTS = {
  // "mine" is the historical default — most members care about their own
  // agents first; admins flip to "all".
  scope: "mine" as AgentsScope,
  sortField: "lastActive" as AgentSortField,
  sortDirection: AGENT_SORT_DEFAULT_DIRECTION.lastActive,
  hiddenColumns: AGENT_DEFAULT_HIDDEN_COLUMNS,
  filters: EMPTY_AGENT_FILTERS,
};

export const useAgentsViewStore = create<AgentsViewState>()(
  persist(
    (set) => ({
      ...DEFAULTS,
      // "Mine" is the clean personal view: entering it clears all filters,
      // so Mine never carries filters. Switching to all/archived leaves
      // filters intact (you can carry "owner = Bob" between them).
      setScope: (scope) =>
        set(scope === "mine" ? { scope, filters: EMPTY_AGENT_FILTERS } : { scope }),
      toggleSort: (field) =>
        set((state) =>
          state.sortField === field
            ? {
                sortDirection: state.sortDirection === "asc" ? "desc" : "asc",
              }
            : {
                sortField: field,
                sortDirection: AGENT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortField: (field) =>
        set((state) =>
          state.sortField === field
            ? {}
            : {
                sortField: field,
                sortDirection: AGENT_SORT_DEFAULT_DIRECTION[field],
              },
        ),
      setSortDirection: (direction) => set({ sortDirection: direction }),
      toggleColumn: (key) =>
        set((state) => ({
          hiddenColumns: state.hiddenColumns.includes(key)
            ? state.hiddenColumns.filter((k) => k !== key)
            : [...state.hiddenColumns, key],
        })),
      toggleFilter: (key, value) =>
        set((state) => {
          const list = state.filters[key] as string[];
          const next = list.includes(value)
            ? list.filter((v) => v !== value)
            : [...list, value];
          // Applying any filter leaves the clean "mine" view for "all" —
          // Mine is the no-filter mode (see setScope). Archived keeps its
          // own scope (it can carry filters).
          const scope = state.scope === "mine" ? "all" : state.scope;
          return { scope, filters: { ...state.filters, [key]: next } };
        }),
      clearFilters: () => set({ filters: EMPTY_AGENT_FILTERS }),
    }),
    {
      name: "multica_agents_view",
      storage: createJSONStorage(() =>
        createWorkspaceAwareStorage(defaultStorage),
      ),
      partialize: (state) => ({
        scope: state.scope,
        sortField: state.sortField,
        sortDirection: state.sortDirection,
        hiddenColumns: state.hiddenColumns,
        filters: state.filters,
      }),
      // On rehydrate, if the new workspace has no persisted value, reset to
      // the defaults instead of leaving the previous workspace's in-memory
      // view state in place. Default merge keeps current state when
      // persisted is undefined, which would leak state across workspaces.
      merge: (persisted, current) => {
        if (!persisted) return { ...current, ...DEFAULTS };
        const p = persisted as Partial<AgentsViewState>;
        // Deep-merge filters so a payload persisted before a new filter
        // dimension existed (e.g. `owners`) still gets that key's default
        // instead of dropping it to `undefined` and crashing `.length`.
        return {
          ...current,
          ...p,
          filters: { ...EMPTY_AGENT_FILTERS, ...(p.filters ?? {}) },
        };
      },
    },
  ),
);

registerForWorkspaceRehydration(() => useAgentsViewStore.persist.rehydrate());
