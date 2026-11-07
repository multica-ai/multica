import { create } from "zustand";
import type { InboxItem, IssuePriority, IssueStatus } from "../types";

export interface InboxFilters {
  readonly statuses: readonly IssueStatus[];
  readonly priorities: readonly IssuePriority[];
  /** Actor keys — see `inboxActorKey`. */
  readonly actors: readonly string[];
  readonly unreadOnly: boolean;
  /** Only issues no agent is running or queued on. Active inbox only. */
  readonly idleOnly: boolean;
}

/**
 * The dimensions the server-filtered archive understands. It has no idle
 * dimension, so `idleOnly` never reaches its endpoints or query keys.
 */
export type ArchivedInboxFilters = Omit<InboxFilters, "idleOnly">;

/**
 * Stable identity for "who this notification came from", used as the value of
 * the `actors` dimension.
 *
 * Members and agents key on their id. `system` has no id — the backend writes
 * an invalid UUID that serializes to null — so it keys on the type alone and
 * every system notification collapses into one bucket. Returns null when the
 * row carries no usable attribution; such a row can never match an actor
 * selection, exactly as a row without an issue can never match a status one.
 */
export function inboxActorKey(item: InboxItem): string | null {
  const type = item.actor_type;
  if (!type) return null;
  if (type === "system") return "system";
  return item.actor_id ? `${type}:${item.actor_id}` : null;
}

/** Inverse of `inboxActorKey`, for resolving a selection back to a directory. */
export function inboxActorKeyParts(key: string): { type: string; id: string } {
  const separator = key.indexOf(":");
  if (separator === -1) return { type: key, id: "" };
  return { type: key.slice(0, separator), id: key.slice(separator + 1) };
}

export type InboxPriorityFilterSupport =
  | "unknown"
  | "supported"
  | "unsupported";

export const EMPTY_INBOX_FILTERS: InboxFilters = Object.freeze({
  statuses: Object.freeze([]),
  priorities: Object.freeze([]),
  actors: Object.freeze([]),
  unreadOnly: false,
  idleOnly: false,
});

/** True when nothing is selected in any dimension. */
export function isEmptyInboxFilters(filters: InboxFilters): boolean {
  return inboxFilterCount(filters) === 0;
}

interface InboxFilterState {
  filtersByWorkspace: Record<string, InboxFilters>;
  toggleStatusFilter: (wsId: string, status: IssueStatus) => void;
  togglePriorityFilter: (wsId: string, priority: IssuePriority) => void;
  toggleActorFilter: (wsId: string, actor: string) => void;
  toggleUnreadOnly: (wsId: string) => void;
  toggleIdleOnly: (wsId: string) => void;
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
  toggleActorFilter: (wsId, actor) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: { ...current, actors: toggleValue(current.actors, actor) },
        },
      };
    }),
  toggleUnreadOnly: (wsId) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: { ...current, unreadOnly: !current.unreadOnly },
        },
      };
    }),
  toggleIdleOnly: (wsId) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId] ?? EMPTY_INBOX_FILTERS;
      return {
        filtersByWorkspace: {
          ...state.filtersByWorkspace,
          [wsId]: { ...current, idleOnly: !current.idleOnly },
        },
      };
    }),
  clearPriorityFilters: (wsId) =>
    set((state) => {
      const current = state.filtersByWorkspace[wsId];
      if (!current || current.priorities.length === 0) return state;
      const next = { ...current, priorities: [] };
      // Emptiness is asked of every dimension, not just statuses: dropping the
      // entry while an actor or unread selection survived in it would clear a
      // filter the user set and the backend never had a say in.
      if (isEmptyInboxFilters(next)) {
        const { [wsId]: _removed, ...filtersByWorkspace } =
          state.filtersByWorkspace;
        return { filtersByWorkspace };
      }
      return {
        filtersByWorkspace: { ...state.filtersByWorkspace, [wsId]: next },
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
  return { ...filters, priorities: [] };
}

const EMPTY_ISSUE_IDS: ReadonlySet<string> = new Set();

/** Page state `filterInboxItems` needs beyond the selected filters. */
export interface InboxFilterContext {
  /** Issues an agent is running or queued on. Read only by `idleOnly`. */
  readonly activeIssueIds?: ReadonlySet<string>;
  /**
   * Group key (`issue_id ?? id`) of the open notification, pinned against the
   * unread dimension only. Set by the page after opening marks the row read.
   */
  readonly pinnedUnreadKey?: string | null;
  /**
   * Group key of the open notification, pinned against the idle dimension
   * only. Set by the page once the row was opened while idle.
   */
  readonly pinnedIdleKey?: string | null;
}

/**
 * The archive is filtered server-side and the backend has no idle dimension,
 * so a selection kept from the active inbox stays dormant there instead of
 * narrowing whichever archived pages happen to be loaded.
 */
export function inboxFiltersForArchive(filters: InboxFilters): InboxFilters {
  return filters.idleOnly ? { ...filters, idleOnly: false } : filters;
}

/**
 * OR within a dimension, AND between dimensions.
 *
 * Every dimension is read off the row the list actually renders. For status
 * and priority that is free — they are properties of the issue, identical on
 * every notification in the group. For the actor it is a deliberate choice:
 * deduplication keeps only a group's newest notification, and that row shows
 * ITS actor's avatar and name. Matching an actor buried in an older row of
 * the same group would answer a different question ("issues Alice has ever
 * touched") than the one the row then displays, so selecting Alice would fill
 * the list with Bob. What you filter by is what you see.
 *
 * `unreadOnly` likewise reads the rendered row's `read`. The unread badge
 * (`useInboxUnreadCount`) counts the same thing from the server's summary
 * endpoint, which applies that newest-per-issue rule in SQL — so "only unread"
 * and the number next to Inbox still cannot disagree.
 *
 * `idleOnly` asks about the issue, like status and priority, so a row without
 * one never matches. `activeIssueIds` are the issues an agent is running or
 * queued on; the caller derives them from the agent task snapshot.
 *
 * `pinnedUnreadKey` / `pinnedIdleKey` name the notification whose detail is
 * open. Opening an unread
 * notification marks it read, and replying from the detail can start an
 * agent on its issue; either would otherwise remove the row the user is
 * working in from under the detail pane. The pinned row therefore survives
 * the unread and idle dimensions only, and each pin only crosses its own
 * dimension: a row pinned for unread is still removed when the idle filter
 * is turned on over a busy issue, and vice versa. The status, priority, and
 * actor dimensions always apply, because changing one of those is an
 * explicit request to filter the notification itself out.
 */
export function filterInboxItems(
  items: InboxItem[],
  filters: InboxFilters,
  {
    activeIssueIds = EMPTY_ISSUE_IDS,
    pinnedUnreadKey = null,
    pinnedIdleKey = null,
  }: InboxFilterContext = {},
): InboxItem[] {
  if (isEmptyInboxFilters(filters)) return items;

  const statuses = new Set(filters.statuses);
  const priorities = new Set(filters.priorities);
  const actors = new Set(filters.actors);
  return items.filter((item) => {
    const actor = inboxActorKey(item);
    const matchesOtherDimensions =
      (statuses.size === 0 ||
        (item.issue_status != null && statuses.has(item.issue_status))) &&
      (priorities.size === 0 ||
        (item.issue_priority != null && priorities.has(item.issue_priority))) &&
      (actors.size === 0 || (actor != null && actors.has(actor)));
    if (!matchesOtherDimensions) return false;
    const key = item.issue_id ?? item.id;
    const matchesUnread =
      !filters.unreadOnly ||
      item.read !== true ||
      (pinnedUnreadKey != null && key === pinnedUnreadKey);
    const matchesIdle =
      !filters.idleOnly ||
      (item.issue_id != null && !activeIssueIds.has(item.issue_id)) ||
      (pinnedIdleKey != null && key === pinnedIdleKey);
    return matchesUnread && matchesIdle;
  });
}

export function inboxFilterCount(filters: InboxFilters): number {
  return (
    filters.statuses.length +
    filters.priorities.length +
    filters.actors.length +
    (filters.unreadOnly ? 1 : 0) +
    (filters.idleOnly ? 1 : 0)
  );
}
