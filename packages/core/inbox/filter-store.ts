import { create } from "zustand";
import { createJSONStorage, persist } from "zustand/middleware";
import { defaultStorage } from "../platform/storage";
import {
  EMPTY_INBOX_FILTERS,
  INBOX_DIMENSION_FIELD,
  INBOX_ISSUE_FIELD_DIMENSIONS,
  inboxFilterValues,
  isEmptyInboxFilters,
  sanitizeInboxFilters,
  toggleInboxFilterValue,
  withoutInboxFilter,
  type InboxFilterKey,
  type InboxFilters,
  type InboxListDimension,
  type InboxReceivedFilter,
} from "./filters";
import type { IssuePriority, IssueStatus } from "../types";

export * from "./filters";

interface InboxFilterState {
  filtersByWorkspace: Record<string, InboxFilters>;
  toggleFilterValue: (wsId: string, dimension: InboxListDimension, value: string) => void;
  setFilterNegated: (wsId: string, dimension: InboxListDimension, negated: boolean) => void;
  toggleStatusFilter: (wsId: string, status: IssueStatus) => void;
  togglePriorityFilter: (wsId: string, priority: IssuePriority) => void;
  toggleActorFilter: (wsId: string, actor: string) => void;
  toggleUnreadOnly: (wsId: string) => void;
  /** Selecting the current value again clears the dimension. */
  setReceived: (wsId: string, received: InboxReceivedFilter) => void;
  clearFilter: (wsId: string, key: InboxFilterKey) => void;
  clearPriorityFilters: (wsId: string) => void;
  clearIssueFieldFilters: (wsId: string) => void;
  clearFilters: (wsId: string) => void;
}

function update(
  state: InboxFilterState,
  wsId: string,
  change: (current: InboxFilters) => InboxFilters,
): Partial<InboxFilterState> | InboxFilterState {
  const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
  const next = change(current);
  if (next === current) return state;
  // Emptiness is asked of every dimension: an entry is dropped only when
  // nothing in it narrows the list any more. An "is not" picked before its
  // first value narrows nothing yet but must survive until that value comes.
  if (isEmptyInboxFilters(next) && next.negated.length === 0) {
    if (!state.filtersByWorkspace[wsId]) return state;
    const { [wsId]: _removed, ...filtersByWorkspace } = state.filtersByWorkspace;
    return { filtersByWorkspace };
  }
  return { filtersByWorkspace: { ...state.filtersByWorkspace, [wsId]: next } };
}

function toggleListValue(current: InboxFilters, dimension: InboxListDimension, value: string): InboxFilters {
  const field = INBOX_DIMENSION_FIELD[dimension];
  // "Is not" stays when the last value goes, as in the issue filter: the
  // operator is the submenu's own setting, and clearing the field resets it.
  return { ...current, [field]: toggleInboxFilterValue(current[field] as readonly string[], value) };
}

export const useInboxFilterStore = create<InboxFilterState>()(
  persist(
    (set) => ({
      filtersByWorkspace: {},
      toggleFilterValue: (wsId, dimension, value) =>
        set((state) => update(state, wsId, (current) => toggleListValue(current, dimension, value))),
      setFilterNegated: (wsId, dimension, negated) =>
        set((state) =>
          update(state, wsId, (current) => {
            if (current.negated.includes(dimension) === negated) return current;
            return {
              ...current,
              negated: negated
                ? [...current.negated, dimension]
                : current.negated.filter((candidate) => candidate !== dimension),
            };
          }),
        ),
      toggleStatusFilter: (wsId, status) =>
        set((state) => update(state, wsId, (current) => toggleListValue(current, "status", status))),
      togglePriorityFilter: (wsId, priority) =>
        set((state) => update(state, wsId, (current) => toggleListValue(current, "priority", priority))),
      toggleActorFilter: (wsId, actor) =>
        set((state) => update(state, wsId, (current) => toggleListValue(current, "actor", actor))),
      toggleUnreadOnly: (wsId) =>
        set((state) => update(state, wsId, (current) => ({ ...current, unreadOnly: !current.unreadOnly }))),
      setReceived: (wsId, received) =>
        set((state) =>
          update(state, wsId, (current) => ({
            ...current,
            received: current.received === received ? null : received,
          })),
        ),
      clearFilter: (wsId, key) =>
        set((state) => update(state, wsId, (current) => withoutInboxFilter(current, key))),
      clearPriorityFilters: (wsId) =>
        set((state) =>
          update(state, wsId, (current) =>
            current.priorities.length === 0 ? current : withoutInboxFilter(current, "priority"),
          ),
        ),
      clearIssueFieldFilters: (wsId) =>
        set((state) =>
          update(state, wsId, (current) =>
            INBOX_ISSUE_FIELD_DIMENSIONS.reduce(
              (next, dimension) =>
                inboxFilterValues(next, dimension).length === 0 ? next : withoutInboxFilter(next, dimension),
              current,
            ),
          ),
        ),
      clearFilters: (wsId) =>
        set((state) => {
          if (!state.filtersByWorkspace[wsId]) return state;
          const { [wsId]: _removed, ...filtersByWorkspace } = state.filtersByWorkspace;
          return { filtersByWorkspace };
        }),
    }),
    {
      // One entry per workspace inside a single key, like the issue view
      // filters: switching workspaces swaps the selection without a reload.
      name: "multica_inbox_filters",
      version: 1,
      storage: createJSONStorage(() => defaultStorage),
      partialize: (state) => ({ filtersByWorkspace: state.filtersByWorkspace }),
      merge: (persisted, current) => {
        const stored = (persisted as { filtersByWorkspace?: unknown } | undefined)?.filtersByWorkspace;
        const filtersByWorkspace: Record<string, InboxFilters> = {};
        if (stored && typeof stored === "object") {
          for (const [wsId, raw] of Object.entries(stored)) {
            const filters = sanitizeInboxFilters(raw);
            if (!isEmptyInboxFilters(filters)) filtersByWorkspace[wsId] = filters;
          }
        }
        return { ...current, filtersByWorkspace };
      },
    },
  ),
);

/** Workspace-isolated filter state with a stable empty fallback. */
export function useInboxFilters(wsId: string): InboxFilters {
  return (
    useInboxFilterStore((state) => state.filtersByWorkspace[wsId]) ??
    EMPTY_INBOX_FILTERS
  );
}
