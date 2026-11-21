import type { IssueStatusCatalog } from "../../issue-statuses/queries";

/**
 * Data a filter needs besides the rows themselves: the viewer's subscriptions,
 * agents or unread list, the running-task projection, the project list, the
 * status catalog. A TanStack Query result fits as is.
 */
export interface FilterDependencySource {
  /** `undefined` until the data arrives. Data kept behind a failed refetch has
   *  arrived: the filter keeps answering from it. */
  data: unknown;
  isError: boolean;
  refetch: () => unknown;
}

export interface FilterDependencyState {
  /** Hold the loading state. */
  pending: boolean;
  /** Show a retryable error. */
  error: boolean;
  /** `pending || error`: the filter has no answer yet. Gate every fetch keyed
   *  by it and every "no results" claim on this. */
  unresolved: boolean;
  /** Re-runs each source that failed. */
  retry: () => void;
}

export const RESOLVED_FILTER_DEPENDENCY: FilterDependencyState = {
  pending: false,
  error: false,
  unresolved: false,
  retry: () => {},
};

/**
 * Whether a filter that reads `sources` can be evaluated.
 *
 * Until every source an active filter reads has data, the filter has no
 * answer: it is pending, or an error once a source has failed. It is never
 * "no filter" (unfiltered rows under an active chip) and never "matches
 * nothing" (an empty list, or a loading state that never ends).
 *
 * `active` is whether this surface applies the filter at all; a `false` entry
 * in `sources` is a source the active filter does not read, so one value of a
 * dimension does not wait on another value's data.
 *
 * Adding a filter dimension that reads extra data means declaring its sources
 * here and gating on `unresolved`, plus three controller regressions: loading
 * while a source is pending, a retryable error when it fails, and correctly
 * filtered rows after a successful retry. A source backed by a new endpoint
 * must reject on a malformed response rather than resolve to an empty list
 * (see `listSubscribedIssueIds`): an empty list is an answer, and the filter
 * would apply it.
 */
export function filterDependencyState(
  active: boolean,
  sources: readonly (FilterDependencySource | false)[],
): FilterDependencyState {
  if (!active) return RESOLVED_FILTER_DEPENDENCY;
  const unresolved = sources.filter(
    (source): source is FilterDependencySource => source !== false && source.data === undefined,
  );
  if (unresolved.length === 0) return RESOLVED_FILTER_DEPENDENCY;
  const failed = unresolved.filter((source) => source.isError);
  return {
    pending: failed.length === 0,
    error: failed.length > 0,
    unresolved: true,
    retry: () => {
      for (const source of failed) void source.refetch();
    },
  };
}

/** Several dependencies behind one loading state and one retry. A failure
 *  anywhere wins over pending elsewhere, and retry re-runs every failure. */
export function combineFilterDependencies(
  ...states: readonly FilterDependencyState[]
): FilterDependencyState {
  const failed = states.filter((state) => state.error);
  const unresolved = states.some((state) => state.unresolved);
  if (!unresolved) return RESOLVED_FILTER_DEPENDENCY;
  return {
    pending: failed.length === 0,
    error: failed.length > 0,
    unresolved: true,
    retry: () => {
      for (const state of failed) state.retry();
    },
  };
}

/** The status catalog as a filter dependency. */
export function statusCatalogSource(
  catalog: Pick<IssueStatusCatalog, "isLoaded" | "statuses" | "isError" | "retry">,
): FilterDependencySource {
  return {
    data: catalog.isLoaded ? catalog.statuses : undefined,
    isError: catalog.isError,
    refetch: catalog.retry,
  };
}
