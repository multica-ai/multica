"use client";

import { useT } from "../../i18n";
import { InfiniteScrollSentinel } from "./infinite-scroll-sentinel";

/** Server row-branch page size (use-issue-status-branches /
 * use-issue-group-branches). A column with no more rows than this never
 * paginated, so its end carries no "no more" marker — the column is
 * self-evidently complete. */
const PAGINATED_THRESHOLD = 50;

/**
 * Unified end-of-column footer for the infinite-scrolled issue surfaces
 * (Board / List / Swimlane). Collapses four copies of the same
 * error / has-more / reached-end ternary into one place so the states and
 * wording stay consistent:
 *  - error       → retry
 *  - has more    → sentinel + "Loading…" label (bare spinner read as "stuck")
 *  - reached end → muted "No more" marker, but only for columns that actually
 *                  paginated (total beyond one page)
 *  - short list  → nothing
 */
export function ListLoadMoreFooter({
  hasMore,
  isLoading,
  total,
  onLoadMore,
  isError = false,
  onRetry,
}: {
  hasMore: boolean;
  isLoading: boolean;
  total: number;
  onLoadMore: () => void;
  isError?: boolean;
  onRetry?: () => void;
}) {
  const { t } = useT("issues");

  if (isError && onRetry) {
    return (
      <button
        type="button"
        className="w-full py-2 text-xs text-destructive hover:underline"
        onClick={onRetry}
      >
        {t(($) => $.table.load_more_failed_retry)}
      </button>
    );
  }

  if (hasMore) {
    return (
      <InfiniteScrollSentinel
        onVisible={onLoadMore}
        loading={isLoading}
        label={t(($) => $.table.loading_branch)}
      />
    );
  }

  if (total > PAGINATED_THRESHOLD) {
    return (
      <div className="py-2 text-center text-xs text-muted-foreground/70">
        {t(($) => $.table.no_more)}
      </div>
    );
  }

  return null;
}
