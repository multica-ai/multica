"use client";

import { useMemo } from "react";
import { Gauge, Minimize2 } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useLocale, useT } from "../../i18n";
import { formatTokens } from "../../runtimes/utils";
import {
  contextUsageHistory,
  contextUsagePercent,
  latestContextUsage,
  type ContextEventSource,
  type ContextObservation,
  type ContextUsage,
} from "./context-usage";

/**
 * How full the model's context window is, for one run.
 *
 * The chip states the current figure; its popover keeps the history, because
 * "it is at 90% now" and "it got there after a compaction eight minutes ago"
 * are different questions and only the second one explains the run. Both read
 * from the same event stream the transcript renders, so the two surfaces can
 * never disagree.
 *
 * Renders nothing when the runtime reported no occupancy: absence means "not
 * reported", and a chip showing an assumed window would read as a measurement.
 */
export function ContextUsageChip({
  items,
  usage: reportedUsage,
  className,
}: {
  /** The run's context events, when this surface has its transcript. */
  items?: readonly ContextEventSource[];
  /**
   * Fallback figure for a surface that has the run but not its transcript —
   * a historical issue run never fetches its messages. The transcript wins
   * when it is loaded: it is the same stream the row was written from, only
   * newer while the run is live.
   */
  usage?: ContextUsage;
  className?: string;
}) {
  const { t } = useT("agents");
  const streamed = useMemo(() => latestContextUsage(items ?? []), [items]);
  const history = useMemo(() => contextUsageHistory(items ?? []), [items]);
  const usage = streamed ?? reportedUsage;

  if (!usage) return null;

  // One observation and nothing else is the figure itself — a popover that
  // repeats the chip is a click that teaches nothing.
  if (history.length <= 1) {
    return <ContextUsageFigure usage={usage} className={className} />;
  }

  return (
    <Popover>
      <PopoverTrigger
        render={
          <button
            type="button"
            // The figure already carries the pill shape, as the cost chip
            // beside it does; the button is only the hit area, so the hover
            // tint goes on the pill rather than on a second shape around it.
            className={cn("group", className)}
            aria-label={t(($) => $.transcript.context_chip_title)}
          >
            <ContextUsageFigure usage={usage} className="group-hover:bg-accent/40" />
          </button>
        }
      />
      <PopoverContent align="end" className="w-72 p-0">
        <p className="border-b px-3 py-2 text-caption font-medium">
          {t(($) => $.transcript.context_history_title)}
        </p>
        <ol className="max-h-64 overflow-auto py-1">
          {[...history].reverse().map((observation, index) => (
            <ContextHistoryRow
              // Observations are append-only and never reordered, so their
              // position in the reversed list is a stable identity even when
              // two share a timestamp.
              key={`${observation.at ?? ""}-${index}`}
              observation={observation}
            />
          ))}
        </ol>
      </PopoverContent>
    </Popover>
  );
}

function ContextUsageFigure({
  usage,
  className,
}: {
  usage: ContextUsage;
  className?: string;
}) {
  const { t } = useT("agents");
  const percent = contextUsagePercent(usage);
  return (
    <span
      className={cn(
        "flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-micro tabular-nums transition-colors",
        className,
      )}
      title={t(($) => $.transcript.context_chip_title)}
    >
      <Gauge aria-hidden="true" className="h-3 w-3 text-muted-foreground" />
      {percent !== undefined && <span className="font-medium">{percent}%</span>}
      <span className="text-muted-foreground">
        {usage.limitTokens !== undefined
          ? `${formatTokens(usage.usedTokens)}/${formatTokens(usage.limitTokens)}`
          : formatTokens(usage.usedTokens)}
      </span>
    </span>
  );
}

function ContextHistoryRow({ observation }: { observation: ContextObservation }) {
  const { t } = useT("agents");
  const locale = useLocale();
  const at = observation.at ? new Date(observation.at) : null;
  const time =
    at && !Number.isNaN(at.getTime())
      ? at.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" })
      : "";

  const compaction = observation.kind === "compaction";
  return (
    <li className="flex items-baseline gap-2 px-3 py-1 text-caption">
      <span className="w-16 shrink-0 font-mono text-micro tabular-nums text-faint-foreground">
        {time}
      </span>
      {compaction ? (
        <span className="flex min-w-0 items-baseline gap-1 text-muted-foreground">
          <Minimize2 aria-hidden="true" className="h-3 w-3 shrink-0 self-center" />
          <span className="truncate">
            {observation.preTokens !== undefined
              ? t(($) => $.transcript.context_compacted_before, {
                  tokens: formatTokens(observation.preTokens),
                })
              : t(($) => $.transcript.context_compacted)}
          </span>
        </span>
      ) : (
        <ContextHistoryFigure observation={observation} />
      )}
    </li>
  );
}

function ContextHistoryFigure({ observation }: { observation: ContextObservation }) {
  if (observation.kind !== "usage") return null;
  const percent = contextUsagePercent(observation);
  return (
    <span className="tabular-nums">
      {percent !== undefined && <span className="font-medium">{percent}% </span>}
      <span className="text-muted-foreground">
        {observation.limitTokens !== undefined
          ? `${formatTokens(observation.usedTokens)}/${formatTokens(observation.limitTokens)}`
          : formatTokens(observation.usedTokens)}
      </span>
    </span>
  );
}
