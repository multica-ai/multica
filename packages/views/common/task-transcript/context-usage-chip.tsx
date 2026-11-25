"use client";

import { useMemo } from "react";
import { Gauge } from "@multica/ui/icons";
import { cn } from "@multica/ui/lib/utils";
import { Popover, PopoverContent, PopoverTrigger } from "@multica/ui/components/ui/popover";
import { useContextUsageSettingsStore } from "@multica/core/agents/stores";
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
 * "it is at 90% now" and "it climbed there in the last two turns" are
 * different questions and only the second one explains the run. Both read
 * from the same event stream the transcript renders, so the two surfaces can
 * never disagree.
 *
 * Renders nothing when the runtime reported no occupancy against a known
 * window — today only Claude and Codex do. Absence means "not reported", and
 * a chip showing an assumed window would read as a measurement.
 */
export function ContextUsageChip({
  items,
  usage: reportedUsage,
  surface,
  onOpenHistory,
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
  /**
   * `issue_row` is the figure on a run's row in an issue timeline, which has
   * its own switch under the feature's own: someone can keep it inside the
   * run and off the row.
   */
  surface?: "issue_row";
  /**
   * Called the first time the history opens, for a surface whose transcript
   * is not loaded until something asks for it — an issue's finished runs
   * deliberately fetch no messages until a reader opens one. Providing it is
   * also what makes the chip openable before any observation has arrived.
   */
  onOpenHistory?: () => void;
  className?: string;
}) {
  const { t } = useT("agents");
  // Off by default, so every surface reads the switch here rather than at each
  // call site, where one could forget and leak the feature.
  const enabled = useContextUsageSettingsStore((state) => state.enabled);
  const showOnIssueRow = useContextUsageSettingsStore((state) => state.showOnIssueRow);
  const streamed = useMemo(() => latestContextUsage(items ?? []), [items]);
  const history = useMemo(() => contextUsageHistory(items ?? []), [items]);
  const usage = streamed ?? reportedUsage;

  if (!enabled) return null;
  if (surface === "issue_row" && !showOnIssueRow) return null;
  if (!usage) return null;

  // A surface with no history to show and no way to fetch one is the figure
  // itself. Everywhere else the chip opens — including on a single
  // observation, so that "is this clickable?" never depends on how many times
  // the runtime happened to report during the run.
  if (history.length === 0 && onOpenHistory === undefined) {
    return <ContextUsageFigure usage={usage} className={className} />;
  }

  return (
    <Popover onOpenChange={(open) => { if (open) onOpenHistory?.(); }}>
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
        {history.length === 0 && (
          <p className="px-3 py-2 text-caption text-muted-foreground">
            {t(($) => $.transcript.context_history_loading)}
          </p>
        )}
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
      <span className="font-medium">{percent}%</span>
      <span className="text-muted-foreground">
        {`${formatTokens(usage.usedTokens)}/${formatTokens(usage.limitTokens)}`}
      </span>
    </span>
  );
}

function ContextHistoryRow({ observation }: { observation: ContextObservation }) {
  const locale = useLocale();
  const at = observation.at ? new Date(observation.at) : null;
  const time =
    at && !Number.isNaN(at.getTime())
      ? at.toLocaleTimeString(locale, { hour: "2-digit", minute: "2-digit", second: "2-digit" })
      : "";

  return (
    <li className="flex items-baseline gap-2 px-3 py-1 text-caption">
      {/* A 12-hour locale appends AM/PM, which wrapped onto a second line in
          the old 4rem column — the figures beside it then sat against a
          two-line row. Wide enough for the longest form, and never wrapped. */}
      <span className="w-20 shrink-0 whitespace-nowrap font-mono text-micro tabular-nums text-faint-foreground">
        {time}
      </span>
      <ContextHistoryFigure observation={observation} />
    </li>
  );
}

function ContextHistoryFigure({ observation }: { observation: ContextObservation }) {
  const percent = contextUsagePercent(observation);
  return (
    <span className="tabular-nums">
      <span className="font-medium">{percent}% </span>
      <span className="text-muted-foreground">
        {`${formatTokens(observation.usedTokens)}/${formatTokens(observation.limitTokens)}`}
      </span>
    </span>
  );
}
