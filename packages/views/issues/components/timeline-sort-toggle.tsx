import { ArrowDownWideNarrow, ArrowUpNarrowWide } from "lucide-react";
import { useTimelineSortStore } from "@multica/core/issues/stores/timeline-sort-store";
import type { TimelineSortDirection } from "@multica/core/issues/timeline-sort";
import { cn } from "@multica/ui/lib/utils";
import { useT } from "../../i18n";

type IssuesT = ReturnType<typeof useT<"issues">>["t"];

interface TimelineSortToggleProps {
  t: IssuesT;
  className?: string;
}

/**
 * Inline Oldest/Newest sort control for the issue timeline header.
 *
 * The query cache is always canonical ASC; this only flips a persisted
 * preference that the display layer reads to reverse top-level groups
 * (activity blocks and root comments). Thread-internal replies stay ASC
 * regardless of direction — see issue-detail.tsx's timelineView.
 */
export function TimelineSortToggle({ t, className }: TimelineSortToggleProps) {
  const direction = useTimelineSortStore((s) => s.direction);
  const setDirection = useTimelineSortStore((s) => s.setDirection);
  const newest = direction === "newest";

  // Cycle: oldest ↔ newest. Two-state toggle, no menu needed.
  const next: TimelineSortDirection = newest ? "oldest" : "newest";
  const label = newest
    ? t(($) => $.detail.sort_newest)
    : t(($) => $.detail.sort_oldest);
  const ariaLabel = t(($) => $.detail.sort_toggle_label);

  return (
    <button
      type="button"
      onClick={() => setDirection(next)}
      aria-label={ariaLabel}
      title={label}
      className={cn(
        "flex items-center gap-1.5 rounded-md px-2 py-1 text-xs text-muted-foreground",
        "hover:bg-muted hover:text-foreground transition-colors",
        className,
      )}
    >
      {newest ? (
        <ArrowUpNarrowWide className="h-3.5 w-3.5" />
      ) : (
        <ArrowDownWideNarrow className="h-3.5 w-3.5" />
      )}
      <span>{label}</span>
    </button>
  );
}
