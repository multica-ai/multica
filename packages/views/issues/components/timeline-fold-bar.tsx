"use client";

import { ChevronDown } from "lucide-react";
import { useT } from "../../i18n";
import { cn } from "@multica/ui/lib/utils";

export const FOLD_LIMIT = 20;

interface TimelineFoldBarProps {
  /** How many items are hidden behind this bar. */
  count: number;
  /** Short date of the earliest hidden item, e.g. "09-30". */
  from: string;
  /** Short date of the latest hidden item, e.g. "10-19". */
  to: string;
  /** Whether the fold is currently expanded. */
  expanded: boolean;
  onToggle: () => void;
}

/**
 * A dashed, 40px bar that replaces the head of a long timeline when the
 * clustered item count exceeds {@link FOLD_LIMIT}. Default collapsed; the
 * earlier items render in place once expanded (no separate scroll container,
 * no lost scroll position — it just un-wraps inline).
 */
export function TimelineFoldBar({
  count,
  from,
  to,
  expanded,
  onToggle,
}: TimelineFoldBarProps) {
  const { t } = useT("issues");
  return (
    <div className="my-4">
      <button
        type="button"
        aria-expanded={expanded}
        onClick={onToggle}
        className="flex h-10 w-full items-center gap-2 rounded-lg border border-dashed border-border px-3 text-left text-label text-muted-foreground transition-colors hover:bg-surface-hover"
      >
        <span className="tabular-nums text-brand">
          {expanded
            ? t(($) => $.detail.timeline.fold_collapse, { count })
            : t(($) => $.detail.timeline.fold_earlier, { count })}
        </span>
        {!expanded && (
          <span className="text-micro tabular-nums text-muted-foreground">
            · {from} – {to}
          </span>
        )}
        <ChevronDown
          className={cn(
            "ml-auto size-3.5 text-muted-foreground transition-transform",
            expanded && "rotate-180",
          )}
        />
      </button>
    </div>
  );
}
