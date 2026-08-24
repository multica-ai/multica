import { create } from "zustand";
import type { InboxItem, IssuePriority, IssueStatus } from "../types";

export interface InboxFilters {
  readonly statuses: readonly IssueStatus[];
  readonly priorities: readonly IssuePriority[];
}

export type InboxPriorityFilterSupport =
  | "unknown"
  | "supported"
  | "unsupported";

export const EMPTY_INBOX_FILTERS: InboxFilters = Object.freeze({
  statuses: Object.freeze([]),
  priorities: Object.freeze([]),
});

interface InboxFilterState {
  filtersByWorkspace: Record<string, InboxFilters>;
  toggleStatusFilter: (wsId: string, status: IssueStatus) => void;
  togglePriorityFilter: (wsId: string, priority: IssuePriority) => void;
  clearPriorityFilters: (wsId: string) => void;
  clearFilters: (wsId: string) => void;
}

function toggleValue<T extends string>(values: readonly T[], value: T): T[] {
  return values.includes(value)
    ? values.filter((candidate) => candidate !== value)
    : [...values, value];
}

export const useInboxFilterStore = create<InboxFilterState>()((set) => ({
  filtersByWorkspace: {},
  toggleStatusFilter: (wsId, status) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: {
            ...current,
            statuses: toggleValue(current.statuses, status),
          },
        },
      };
    }),
  togglePriorityFilter: (wsId, priority) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: {
            ...current,
            priorities: toggleValue(current.priorities, priority),
          },
        },
      };
    }),
  clearPriorityFilters: (wsId) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId];
      if (!current || current.priorities.length === 0) return state;
      if (current.statuses.length === 0) {
        const { [wsId]: _removed, ...filtersByWorkspace } =
          state.filtersByWorkspace;
        return { filtersByWorkspace };
      }
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: { ...current, priorities: [] },
        },
      };
    }),
  clearFilters: (wsId) =>
    set((state) => {
      if (!state.filtersByWorkspace[wsId]) return state;
      const { [wsId]: _removed, ...filtersByWorkspace } =
        state.filtersByWorkspace;
      return { filtersByWorkspace };
    }),
}));

/** Workspace-isolated filter state with a stable empty fallback. */
export function useInboxFilters(wsId: string): InboxFilters {
  return (
    useInboxFilterStore((state) => state.filtersByWorkspace[wsId]) ??
    EMPTY_INBOX_FILTERS
  );
}

/**
 * Capability inferred from the parsed response, not from a version string.
 *
 * The new backend always serializes `issue_priority` for every Inbox row
 * (including `null` for notifications without an issue). An older backend
 * omits it. Requiring every returned row to carry a defined value also keeps a
 * priority cache patch from making a mixed rolling-deploy response look fully
 * supported.
 */
export function inboxPriorityFilterSupport(
  items: readonly InboxItem[],
): InboxPriorityFilterSupport {
  if (items.length === 0) return "unknown";
  return items.every((item) => item.issue_priority !== undefined)
    ? "supported"
    : "unsupported";
}

/** Ignore a stale priority selection until the backend capability is proven. */
export function inboxFiltersForPrioritySupport(
  filters: InboxFilters,
  support: InboxPriorityFilterSupport,
): InboxFilters {
  if (support === "supported" || filters.priorities.length === 0) {
    return filters;
  }
  return { statuses: filters.statuses, priorities: [] };
}

/** OR within a dimension, AND between status and priority dimensions. */
export function filterInboxItems(
  items: InboxItem[],
  filters: InboxFilters,
): InboxItem[] {
  if (filters.statuses.length === 0 && filters.priorities.length === 0) {
    return items;
  }

  const statuses = new Set(filters.statuses);
  const priorities = new Set(filters.priorities);
  return items.filter(
    (item) =>
      (statuses.size === 0 ||
        (item.issue_status != null && statuses.has(item.issue_status))) &&
      (priorities.size === 0 ||
        (item.issue_priority != null && priorities.has(item.issue_priority))),
  );
}

export function inboxFilterCount(filters: InboxFilters): number {
  return filters.statuses.length + filters.priorities.length;
}
