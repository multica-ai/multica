import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { STATUS_CONFIG, STATUS_ORDER } from "../issues/config";
import type { IssueStatusCategory, IssueStatusEntry } from "../types";

/**
 * The workspace issue status catalog (MUL-6243).
 *
 * A workspace always has the 7 built-in statuses and may define custom ones.
 * Every status — built-in or custom — belongs to exactly one of the 7
 * CATEGORIES, and the category is what determines platform behavior, board
 * column and presentation. So this catalog answers one question for the UI:
 * given a status key stored on an issue, what is its label, color and category?
 */

export const issueStatusKeys = {
  all: (wsId: string) => ["issue-statuses", wsId] as const,
  list: (wsId: string) => [...issueStatusKeys.all(wsId), "list"] as const,
};

export function issueStatusListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueStatusKeys.list(wsId),
    // ARCHIVED entries included on purpose. Archiving retires a status from
    // future assignment but leaves existing issues on it, and those issues must
    // keep their real name, color and category. Dropping archived rows here
    // would degrade them to a raw key with a guessed category. Pickers use
    // `activeStatuses`, which excludes them. (MUL-6243)
    queryFn: () => api.listIssueStatuses(true),
    select: (data) => data.statuses,
    // The catalog changes only when an admin edits it, which is rare, so a
    // generous stale time keeps this off the critical path of every render
    // that needs a status label.
    staleTime: 5 * 60_000,
  });
}

/**
 * A resolved view over the catalog. Every lookup falls back to something
 * renderable, because an issue can legitimately carry a status this client has
 * not heard of: a status created moments ago in another session, or one whose
 * catalog fetch has not landed yet.
 */
export interface IssueStatusCatalog {
  /**
   * Every status in display order, ARCHIVED INCLUDED, so an issue left on an
   * archived status still resolves to its real name, color and category.
   * For anything that offers a status to pick, use `activeStatuses`.
   */
  statuses: IssueStatusEntry[];
  /** Assignable statuses — `statuses` minus archived ones. */
  activeStatuses: IssueStatusEntry[];
  /** Category for a status key; falls back to the key when it is a built-in, else "todo". */
  categoryOf: (statusKey: string) => IssueStatusCategory;
  /** Human label for a status key; falls back to the category label, then the raw key. */
  labelOf: (statusKey: string) => string;
  /** Catalog entry for a status key, when the catalog knows it. */
  entryOf: (statusKey: string) => IssueStatusEntry | undefined;
  /** ACTIVE statuses belonging to one category, in display order. */
  inCategory: (category: IssueStatusCategory) => IssueStatusEntry[];
  /** True once the catalog has loaded; false while it is still in flight. */
  isLoaded: boolean;
  /**
   * The catalog is still in flight and nothing has resolved yet.
   *
   * A surface that routes on a CUSTOM status key has to hold its loading state
   * while this is true. `isLoaded === false` alone is not enough to act on:
   * the difference between "not here yet" and "the request failed" decides
   * whether the user sees a spinner or a retryable error, and the first cut
   * showed neither — just a silently empty board. (MUL-6243)
   */
  isPending: boolean;
  /**
   * The catalog request failed AND there is no usable snapshot to fall back on.
   *
   * Narrower than "the query errored" on purpose: a BACKGROUND refetch can fail
   * while the last successful catalog is still cached, and blocking a surface
   * then would throw away data that is perfectly serviceable. Only a failure
   * with nothing behind it is worth stopping for. (MUL-6243)
   */
  isError: boolean;
  /** Re-runs the catalog request. Wired to the surface's retry affordance. */
  retry: () => void;
  /**
   * True when the catalog is LOADED and holds at least one custom status.
   *
   * This is the switch for the category-grouped surface contract. Two reasons
   * it has to be both conditions:
   *
   * - Rolling deploy. `group.kind=status_category` is a server contract this
   *   feature introduced, so a client that sends it unconditionally 400s
   *   against any pod that has not been updated yet. A workspace only HAS a
   *   custom status once the creation flag was on, which means the fleet was
   *   already serving this version.
   * - Cold load. Until the catalog lands, `categoryOf` cannot tell a custom key
   *   from an unknown one and falls back to `todo`. Routing on that guess sends
   *   a saved `qa` filter to the todo column and renders an empty board.
   *
   * A workspace with no custom statuses therefore keeps the exact request it
   * made before this feature — which is also what keeps its board off the extra
   * catalog reads the category contract needs. (MUL-6243)
   */
  hasCustomStatuses: boolean;
}

const BUILT_IN = new Set<string>(STATUS_ORDER);

const CATEGORY_RANK = new Map<string, number>(STATUS_ORDER.map((c, i) => [c, i]));

/**
 * The server's catalog ordering, mirrored for client-side re-sorts.
 * Category rank, then intra-category position, then key as a stable tiebreak —
 * see `ListIssueStatusEntries` in `issue_status.sql`. An optimistic reorder has
 * to re-sort with this or the new positions land in the cache while the list
 * still renders in the old order.
 */
export function compareIssueStatusEntries(a: IssueStatusEntry, b: IssueStatusEntry): number {
  const rank =
    (CATEGORY_RANK.get(a.category) ?? STATUS_ORDER.length) -
    (CATEGORY_RANK.get(b.category) ?? STATUS_ORDER.length);
  if (rank !== 0) return rank;
  if (a.position !== b.position) return a.position - b.position;
  return a.key.localeCompare(b.key);
}

export function isIssueStatusCategory(value: string): value is IssueStatusCategory {
  return BUILT_IN.has(value);
}

/**
 * Builds the resolved catalog from a raw entry list. Pure, so the store and
 * other non-React callers can use it with a list they already hold.
 */
export function buildIssueStatusCatalog(
  entries: IssueStatusEntry[] | undefined,
  status: { isPending?: boolean; isError?: boolean; retry?: () => void } = {},
): IssueStatusCatalog {
  const list = entries ?? [];
  const byKey = new Map(list.map((e) => [e.key, e]));

  const categoryOf = (statusKey: string): IssueStatusCategory => {
    const category = byKey.get(statusKey)?.category;
    if (category && isIssueStatusCategory(category)) return category;
    // A built-in key IS its own category, so an unloaded catalog still resolves
    // all 7 correctly — which is what keeps the default workspace rendering
    // identically before the fetch lands.
    if (isIssueStatusCategory(statusKey)) return statusKey;
    // An unknown custom key: render it somewhere sane rather than dropping it.
    return "todo";
  };

  return {
    statuses: list,
    activeStatuses: list.filter((e) => !e.archived_at),
    categoryOf,
    entryOf: (statusKey) => byKey.get(statusKey),
    labelOf: (statusKey) => {
      const entry = byKey.get(statusKey);
      if (entry) return entry.name;
      if (isIssueStatusCategory(statusKey)) return STATUS_CONFIG[statusKey]?.label ?? statusKey;
      return statusKey;
    },
    inCategory: (category) => list.filter((e) => e.category === category && !e.archived_at),
    isLoaded: entries !== undefined,
    // Defaults describe a non-React caller holding a list it already has:
    // resolved when entries are present, still pending when they are not.
    isPending: status.isPending ?? entries === undefined,
    // A failure that still has entries behind it is a stale-data situation, not
    // a blocking one.
    isError: (status.isError ?? false) && entries === undefined,
    retry: status.retry ?? (() => {}),
    hasCustomStatuses:
      entries !== undefined && list.some((e) => e.is_system !== true),
  };
}
