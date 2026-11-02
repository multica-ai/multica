"use client";

import { useId, useMemo, useRef, useState } from "react";
import { motion, useReducedMotion } from "motion/react";
import { ArrowRight, Ban, ChevronRight, XCircle } from "@multica/ui/icons";
import type { AgentTask } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import { TruncatedTooltip } from "@multica/ui/components/common/truncated-tooltip";
import { useActorName } from "@multica/core/workspace/hooks";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { ActorAvatar } from "../../common/actor-avatar";
import { DeferredTooltip } from "../../common/deferred-tooltip";
import { TranscriptButton } from "../../common/task-transcript";
import { cancellationActorLabel, cancelReasonLabel, failureReasonLabel } from "../../agents/components/tabs/task-failure";
import { Segmented } from "../../dashboard/components/dashboard-shared";
import { formatDuration } from "../../dashboard/utils";
import { useLocale, useT } from "../../i18n";
import {
  collectUnmappedModels,
  formatTokens,
  formatUsd,
  type CostBreakdown,
} from "../../runtimes/utils";
import { AttributionBadge } from "./attribution-badge";
import {
  buildRunTimeline,
  cacheHitRate,
  groupRunsByDay,
  layoutCostFlow,
  nearestPoint,
  niceTicks,
  shortModelName,
  spreadLabels,
  timeTicks,
  type CostShare,
  type FlowLink,
  type RunTimeline,
  type TimeTick,
  type TimelineRun,
} from "./issue-run-timeline";
import { RunCumulativeChart } from "./issue-runs-cumulative-chart";
import { canRetryRun, RetryRunButton } from "./retry-run-button";
import { RunTriggerLabel } from "./run-trigger-label";
import { agentColorMap, markStyle, OTHERS_KEY, SERIES_COLORS } from "./run-chart-colors";
import { useStatusLabel, useTriggerText } from "./task-run-labels";

// The issue's runs laid out in time — the surface the execution log's header
// and per-run bars open.
//
// The sidebar answers "what is running and what just ran"; this answers "how
// did this issue get here": what it spent and on what, when, who asked for
// each run, and which ones were expensive. The chart on top is one dot per
// run on the issue's time axis (cost, tokens or agent time), then where the
// spend went from agents to models; the list below is the same runs as rows,
// newest first — it is also the chart's accessible, exact-value view. The
// cumulative cost curve over time is the chart's second view.
//
// Every figure comes from the same `summarizeTaskUsage` helpers the sidebar
// uses, so the total here can never disagree with the total that opened it.

// Cost categories in the order and colours the runtime usage charts stack them
// (`costStackConfig` in runtimes/components/charts/daily-cost-chart.tsx), so a
// run's split reads the same as a day's bar on the usage page.
const COST_PARTS = [
  { key: "input", swatch: "bg-chart-1" },
  { key: "output", swatch: "bg-chart-2" },
  { key: "cacheRead", swatch: "bg-chart-4" },
  { key: "cacheWrite", swatch: "bg-chart-3" },
] as const satisfies readonly { key: keyof CostBreakdown; swatch: string }[];

type CostPartKey = (typeof COST_PARTS)[number]["key"];

// Flex grow factors that sum below 1 fill only that fraction of the free
// space (CSS Flexbox §7.1), so splitting a bar by raw dollar amounts under $1
// would draw it short. As shares of the total they always sum to 1.
function shareStyle(value: number, total: number): React.CSSProperties {
  return { flexGrow: total > 0 ? value / total : 0, flexBasis: 0 };
}

function sumOf(values: readonly number[]): number {
  return values.reduce((sum, v) => sum + v, 0);
}

function useCostPartLabel(): (key: CostPartKey) => string {
  const { t } = useT("issues");
  return (key) => {
    switch (key) {
      case "input": return t(($) => $.runs_timeline.cost_input);
      case "output": return t(($) => $.runs_timeline.cost_output);
      case "cacheRead": return t(($) => $.runs_timeline.cost_cache_read);
      case "cacheWrite": return t(($) => $.runs_timeline.cost_cache_write);
    }
  };
}

function usePercent(): (share: number) => string {
  const locale = useLocale();
  const fmt = useMemo(
    () => new Intl.NumberFormat(locale, { style: "percent", maximumFractionDigits: 0 }),
    [locale],
  );
  return (share) => fmt.format(share);
}

export function IssueRunsDialog({
  open,
  onOpenChange,
  issueId,
  identifier,
  issueTitle,
  tasks,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  issueId: string;
  identifier: string;
  issueTitle?: string;
  tasks: AgentTask[];
}) {
  const { t } = useT("issues");
  // `estimateCost` reads custom rates imperatively out of the Zustand store,
  // so nothing re-renders this dialog when the user saves a new rate. Subscribe
  // to the snapshot and carry it into every memo that prices usage — same
  // reason the runtime usage page subscribes in usage-section.tsx.
  const pricings = useCustomPricingStore((s) => s.pricings);
  // Active runs stretch to "now". The clock is re-read when the task list
  // changes or the dialog reopens rather than ticking: the dots move by
  // minutes, and a live timer already runs in the sidebar row.
  const timeline = useMemo(
    () => buildRunTimeline(tasks, Date.now()),
    // `pricings` re-prices on a saved custom rate; `open` re-reads the clock.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [tasks, pricings, open],
  );
  // Models with no rate-table entry and no provider-reported cost: their tokens
  // are counted but their spend is not, so the totals understate reality.
  const unmapped = useMemo(
    () => collectUnmappedModels(tasks.flatMap((task) => task.usage ?? [])),
    // eslint-disable-next-line react-hooks/exhaustive-deps -- `pricings` changes which models are priced
    [tasks, pricings],
  );

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      {/* DialogContent's base is `sm:max-w-sm`, which a same-specificity
          `max-w-5xl` does not beat — `!` wins it, matching the transcript
          dialog. Fixed height so the chart stays put while the list scrolls. */}
      <DialogContent className="flex !h-[min(56rem,calc(100dvh-4rem))] !w-[calc(100vw-4rem)] !max-w-5xl flex-col !gap-0 overflow-hidden !p-0">
        <DialogHeader className="px-6 pt-5 pb-4">
          <DialogTitle>{t(($) => $.runs_timeline.title)}</DialogTitle>
          <DialogDescription>
            {issueTitle ? `${identifier} · ${issueTitle}` : identifier}
          </DialogDescription>
        </DialogHeader>

        {timeline.runs.length > 0 && (
          <>
            <RunStats timeline={timeline} />
            <RunCharts timeline={timeline} />
            {timeline.pricedCount > 0 && <CostFlow timeline={timeline} />}
            {/* `min-h-0`: the dialog is a flex column; without it this flex
                item sizes to its content and the list never scrolls. */}
            <div className="min-h-0 flex-1 overflow-y-auto px-6 pb-5">
              <RunDayList timeline={timeline} issueId={issueId} />
              <div className="mt-4 space-y-1 text-micro text-muted-foreground">
                {unmapped.length > 0 && (
                  <p>{t(($) => $.runs_timeline.note_unmapped, { models: unmapped.join(", ") })}</p>
                )}
                <p>{t(($) => $.runs_timeline.note_estimate)}</p>
              </div>
            </div>
          </>
        )}
      </DialogContent>
    </Dialog>
  );
}

// ─── Stats ─────────────────────────────────────────────────────────────────

function Stat({
  label,
  children,
}: {
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div role="group" aria-label={label} className="min-w-0">
      <div aria-hidden className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
        {label}
      </div>
      <div className="mt-1">{children}</div>
    </div>
  );
}

// `formatDuration` falls back to this label only below one second.
const UNDER_A_SECOND = "0s";

function RunStats({ timeline }: { timeline: RunTimeline }) {
  const { t } = useT("issues");
  const percent = usePercent();
  const priced = timeline.pricedCount > 0;
  const hit = cacheHitRate(timeline.tokens);
  const breakdown = [
    timeline.failedCount > 0 &&
      t(($) => $.runs_timeline.count_failed, { count: timeline.failedCount }),
    timeline.cancelledCount > 0 &&
      t(($) => $.runs_timeline.count_cancelled, { count: timeline.cancelledCount }),
    timeline.activeCount > 0 &&
      t(($) => $.runs_timeline.count_active, { count: timeline.activeCount }),
  ].filter(Boolean);

  return (
    <div className="flex flex-wrap items-end gap-x-8 gap-y-3 px-6">
      <Stat label={t(($) => $.runs_timeline.stat_spent)}>
        {/* Proportional figures: a standalone hero number set in tabular
            digits reads loose. "—" when nothing reported usage — never $0. */}
        <span className="text-display-sm font-semibold">
          {priced ? formatUsd(timeline.totalCost) : "—"}
        </span>
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_tokens)}>
        <span className="text-title-sm font-medium tabular-nums">
          {priced ? formatTokens(timeline.tokens.total) : "—"}
        </span>
        {/* Cache reads are most of an agent's tokens; without the share the
            total reads as far more fresh work than it was. */}
        {priced && hit != null && (
          <span className="ml-1.5 text-caption text-muted-foreground">
            · {t(($) => $.runs_timeline.tokens_cached, { percent: percent(hit) })}
          </span>
        )}
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_output)}>
        <span className="text-title-sm font-medium tabular-nums">
          {priced ? formatTokens(timeline.tokens.output) : "—"}
        </span>
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_agent_time)}>
        <span className="text-title-sm font-medium">
          {formatDuration(timeline.agentMs / 1000, UNDER_A_SECOND)}
        </span>
        <span className="ml-1.5 text-caption text-muted-foreground">
          · {t(($) => $.runs_timeline.elapsed_note, {
            duration: formatDuration(timeline.elapsedMs / 1000, UNDER_A_SECOND),
          })}
        </span>
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_runs)}>
        <span className="text-title-sm font-medium">{timeline.runs.length}</span>
        {breakdown.length > 0 && (
          <span className="ml-1.5 text-caption text-muted-foreground">
            · {breakdown.join(" · ")}
          </span>
        )}
      </Stat>
    </div>
  );
}

// ─── Chart ─────────────────────────────────────────────────────────────────

type ChartView = "runs" | "cumulative";

function RunCharts({ timeline }: { timeline: RunTimeline }) {
  const { t } = useT("issues");
  const [view, setView] = useState<ChartView>("runs");
  const viewSwitch = (
    <Segmented
      label={t(($) => $.runs_timeline.view_label)}
      value={view}
      onChange={setView}
      options={[
        { value: "runs", label: t(($) => $.runs_timeline.view_runs) },
        { value: "cumulative", label: t(($) => $.runs_timeline.view_cumulative) },
      ]}
    />
  );
  return view === "runs" ? (
    <RunActivityChart timeline={timeline} viewSwitch={viewSwitch} />
  ) : (
    <RunCumulativeChart timeline={timeline} viewSwitch={viewSwitch} />
  );
}

type Metric = "cost" | "tokens" | "time";

const PLOT_HEIGHT = 168;
// Dot diameter follows the run's duration (area-true: by square root), so a
// long cheap run and a short cheap run stay distinguishable at a glance.
const DOT_MIN = 8;
const DOT_MAX = 22;
// For agent time the y axis already is the duration; every dot is this size.
const DOT_FIXED = 12;
// A run that reported nothing for the metric sits on the baseline at this
// size, hatched, so it never reads as a small figure.
const DOT_NO_FIGURE = 7;
// The hover card's `max-w-72` plus its gap from the dot. With less room than
// this to the right, the card opens on its left.
const HOVER_CARD_ROOM_PX = 298;
const HOUR_MS = 60 * 60 * 1000;

// One dot per run on the issue's time axis — where it sits across is when it
// ran, how high is what it cost, used or took, its size how long it ran, its
// colour who ran it. Bursts read as clusters, an expensive run stands clear
// of them, and the average line says where "normal" is. Failed runs are red
// wherever they land.
function RunActivityChart({
  timeline,
  viewSwitch,
}: {
  timeline: RunTimeline;
  /** The dialog's chart switch, placed first in this chart's toolbar. */
  viewSwitch: React.ReactNode;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const reduceMotion = useReducedMotion();
  const [metric, setMetric] = useState<Metric>("cost");
  const runs = timeline.runs;
  const [d0, d1] = timeline.domain;
  const xPct = (ms: number) => ((ms - d0) / (d1 - d0)) * 100;

  const colors = useMemo(() => agentColorMap(timeline), [timeline]);
  const legend = useMemo(() => {
    const list: { key: string; label: string; color: string | null }[] = [];
    let others = false;
    for (const [agentId, color] of colors) {
      if (color == null) others = true;
      else list.push({ key: agentId, label: getActorName("agent", agentId), color });
    }
    if (others) list.push({ key: OTHERS_KEY, label: t(($) => $.runs_timeline.others), color: null });
    return list;
  }, [colors, getActorName, t]);

  const valueOf = (run: TimelineRun): number | null =>
    run.active ? null : metric === "cost" ? (run.usage?.cost ?? null) : metric === "tokens" ? (run.usage?.tokens ?? null) : run.durationMs;
  const measuredValues = runs.map(valueOf).filter((v): v is number => v != null && v > 0);
  const maxValue = Math.max(0, ...measuredValues);
  // Agent time ticks in whole hours once a run takes an hour, else minutes.
  const timeUnitMs = metric === "time" && maxValue < HOUR_MS ? 60_000 : HOUR_MS;
  const yTicks =
    metric === "time"
      ? niceTicks(maxValue / timeUnitMs).map((v) => v * timeUnitMs)
      : niceTicks(maxValue);
  const yMax = yTicks[yTicks.length - 1] ?? 1;
  const yPct = (v: number) => (1 - v / yMax) * 100;
  const formatValue = (v: number) =>
    metric === "cost"
      ? formatTick(v)
      : metric === "tokens"
        ? formatTokens(v)
        : `${Number((v / timeUnitMs).toPrecision(3))}${timeUnitMs === HOUR_MS ? "h" : "m"}`;
  const average = measuredValues.length >= 2 ? sumOf(measuredValues) / measuredValues.length : null;
  const averageLabel =
    average == null ? "" : metric === "cost" ? formatUsd(average) : metric === "tokens" ? formatTokens(average) : formatDuration(average / 1000, UNDER_A_SECOND);

  const maxDuration = Math.max(0, ...runs.map((run) => run.durationMs ?? 0));
  const dots = runs.map((run) => {
    const value = valueOf(run);
    const measured = value != null && value > 0;
    return {
      run,
      measured,
      x: xPct((run.startMs + run.endMs) / 2),
      y: measured ? yPct(value) : 100,
      size: !measured
        ? DOT_NO_FIGURE
        : metric === "time"
          ? DOT_FIXED
          : DOT_MIN + (DOT_MAX - DOT_MIN) * Math.sqrt(maxDuration > 0 ? (run.durationMs ?? 0) / maxDuration : 0),
    };
  });

  const ticks = timeTicks(timeline.domain);
  const tickFormat = useMemo(() => {
    const day = new Intl.DateTimeFormat(locale, { month: "short", day: "numeric" });
    const hour = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false });
    const today = new Date().toDateString();
    // An hour tick on midnight names the day instead, so a short span still
    // says which side of it is today.
    return (tick: TimeTick) =>
      tick.kind === "hour" && new Date(tick.t).getHours() !== 0
        ? hour.format(tick.t)
        : new Date(tick.t).toDateString() === today
          ? t(($) => $.runs_timeline.day_today)
          : day.format(tick.t);
  }, [locale, t]);

  // Hover layer: the pointer picks the nearest dot anywhere in the chart row,
  // walking run to run along the time axis. The list below carries the same
  // figures for keyboard and screen-reader users.
  const plotRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<{ index: number; width: number } | null>(null);
  const trackPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = plotRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return;
    const index = nearestPoint(
      dots.map((dot) => ({ x: (dot.x / 100) * rect.width, y: (dot.y / 100) * PLOT_HEIGHT })),
      event.clientX - rect.left,
      event.clientY - rect.top,
    );
    setHover(index >= 0 ? { index, width: rect.width } : null);
  };
  const hoveredDot = hover ? dots[hover.index] : undefined;
  const cardOnLeft = hover && hoveredDot ? (hoveredDot.x / 100) * hover.width + HOVER_CARD_ROOM_PX > hover.width : false;

  return (
    <div className="mt-5 border-b px-6 pb-3">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        {viewSwitch}
        <Segmented
          label={t(($) => $.runs_timeline.metric_label)}
          value={metric}
          onChange={setMetric}
          options={[
            { value: "cost", label: t(($) => $.runs_timeline.metric_cost) },
            { value: "tokens", label: t(($) => $.runs_timeline.metric_tokens) },
            { value: "time", label: t(($) => $.runs_timeline.metric_time) },
          ]}
        />
        <div className="ml-auto flex flex-wrap items-center gap-x-3 gap-y-1 text-caption text-muted-foreground">
          {legend.map((entry) => (
            <span key={entry.key} className="flex items-center gap-1.5">
              <span
                aria-hidden
                className={cn("size-2.5 rounded-full", entry.color ? "chart-mark" : "chart-mark-neutral")}
                style={entry.color ? markStyle(entry.color) : undefined}
              />
              {entry.label}
            </span>
          ))}
          {metric !== "time" && <span>{t(($) => $.runs_timeline.size_is_duration)}</span>}
        </div>
      </div>

      <div
        role="img"
        aria-label={t(($) => $.runs_timeline.chart_aria, {
          count: runs.length,
          cost: formatUsd(timeline.totalCost),
        })}
        className="flex gap-3"
        onPointerMove={trackPointer}
        onPointerDown={trackPointer}
        onPointerLeave={() => setHover(null)}
      >
        <div ref={plotRef} className="relative min-w-0 flex-1">
          <div
            className="relative rounded-md bg-[radial-gradient(circle,var(--chart-bar-ring)_1px,transparent_1.25px)] bg-[length:14px_14px] bg-center"
            style={{ height: PLOT_HEIGHT }}
            aria-hidden
          >
            <span className="absolute inset-x-0 bottom-0 h-px bg-border" />
            {average != null && (
              <span
                className="absolute inset-x-0 border-t border-dashed border-muted-foreground/50"
                style={{ top: `${yPct(average)}%` }}
              />
            )}
            {hoveredDot && (
              <span
                className="absolute top-0 bottom-0 border-l border-dashed border-foreground/30"
                style={{ left: `${hoveredDot.x}%` }}
              />
            )}
            {dots.map((dot, i) => {
              const color = colors.get(dot.run.task.agent_id) ?? null;
              const failed = dot.run.task.status === "failed";
              const tone =
                dot.run.active && !dot.measured
                  ? "bg-info motion-safe:animate-pulse"
                  : failed
                    ? "chart-mark"
                    : !dot.measured
                      ? "chart-mark-hatch"
                      : color
                        ? "chart-mark"
                        : "chart-mark-neutral";
              return (
                <motion.span
                  key={dot.run.task.id}
                  data-run={dot.run.task.id}
                  initial={reduceMotion ? false : { scale: 0 }}
                  animate={{ scale: 1 }}
                  transition={{ duration: 0.4, delay: Math.min(i, 40) * 0.015, ease: [0.22, 1, 0.36, 1] }}
                  className={cn(
                    "absolute rounded-full transition-opacity",
                    tone,
                    hoveredDot && hoveredDot !== dot && "opacity-35",
                    hoveredDot === dot && "z-10",
                  )}
                  style={{
                    left: `calc(${dot.x}% - ${dot.size / 2}px)`,
                    top: `calc(${dot.y}% - ${dot.size / 2}px)`,
                    width: dot.size,
                    height: dot.size,
                    ...(tone === "chart-mark" ? markStyle(failed ? "var(--destructive)" : color!) : null),
                  }}
                />
              );
            })}
          </div>

          {hoveredDot && hover && (
            <div
              aria-hidden
              data-hover-card
              className="pointer-events-none absolute z-20 w-max max-w-72 rounded-lg border bg-popover px-2.5 py-1.5 text-caption text-popover-foreground shadow-[var(--menu-shadow)]"
              style={{
                left: `${hoveredDot.x}%`,
                top: hoveredDot.y > 50 ? 0 : Math.round(PLOT_HEIGHT * 0.45),
                transform: cardOnLeft ? "translateX(calc(-100% - 14px))" : "translateX(14px)",
              }}
            >
              <RunHoverCard run={hoveredDot.run} parts={metric === "tokens"} />
            </div>
          )}

          <div className="relative mt-px h-5" aria-hidden>
            {ticks.map((tick) => {
              const x = xPct(tick.t);
              return (
                <span
                  key={tick.t}
                  className="absolute top-1 whitespace-nowrap px-1 text-micro text-muted-foreground"
                  style={{ left: `${x}%`, transform: x > 92 ? "translateX(-100%)" : undefined }}
                >
                  {tickFormat(tick)}
                </span>
              );
            })}
          </div>
        </div>

        {/* Y scale on the right; the average takes the scale where it falls,
            so it never sits on the dots. */}
        <div className="relative w-16 shrink-0 text-micro tabular-nums" style={{ height: PLOT_HEIGHT }} aria-hidden>
          {yTicks.map((v) =>
            average != null && Math.abs(yPct(v) - yPct(average)) < 10 ? null : (
              <span
                key={v}
                className="absolute left-1 -translate-y-1/2 text-muted-foreground"
                style={{ top: `${yPct(v)}%` }}
              >
                {formatValue(v)}
              </span>
            ),
          )}
          {average != null && (
            <span
              className="absolute left-0 -translate-y-1/2 whitespace-nowrap rounded-full bg-foreground px-1.5 font-medium text-background"
              style={{ top: `${yPct(average)}%` }}
            >
              {t(($) => $.runs_timeline.average, { value: averageLabel })}
            </span>
          )}
        </div>
      </div>
    </div>
  );
}

// Axis steps are round numbers; "$50.00" would print noise a tick does not need.
function formatTick(v: number): string {
  return Number.isInteger(v) ? `$${v}` : formatUsd(v);
}

function RunHoverCard({ run, parts }: { run: TimelineRun; parts: boolean }) {
  const { t } = useT("issues");
  const partLabel = useCostPartLabel();
  const { getActorName } = useActorName();
  const trigger = useTriggerText(run.task);
  const status = useStatusLabel(run.task.status);
  const locale = useLocale();
  const when = new Intl.DateTimeFormat(locale, {
    month: "short",
    day: "numeric",
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(run.startMs);
  const facts = [
    when,
    run.task.status === "completed" && run.durationMs != null
      ? formatDuration(run.durationMs / 1000, UNDER_A_SECOND)
      : status,
  ];
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <RunTriggerLabel task={run.task} fallback={trigger}>
        {(label) => <span className="truncate font-medium">{label}</span>}
      </RunTriggerLabel>
      <span className="flex min-w-0 items-center gap-1.5 text-micro text-muted-foreground">
        <ActorAvatar actorType="agent" actorId={run.task.agent_id} size="xs" />
        <span className="truncate">{[getActorName("agent", run.task.agent_id), ...facts].join(" · ")}</span>
      </span>
      <span className="text-micro tabular-nums">
        {run.usage ? (
          <>
            <span className="font-medium">{formatUsd(run.usage.cost)}</span>
            <span className="text-muted-foreground">
              {" · "}
              {t(($) => $.runs_timeline.tokens_count, { tokens: formatTokens(run.usage.tokens) })}
            </span>
          </>
        ) : (
          <span className="text-muted-foreground">{t(($) => $.runs_timeline.no_usage)}</span>
        )}
      </span>
      {parts &&
        run.usage &&
        COST_PARTS.map((part) =>
          run.usage![part.key] > 0 ? (
            <span key={part.key} className="flex items-center gap-1.5 text-micro tabular-nums">
              <span aria-hidden className={cn("size-2 shrink-0 rounded-xs", part.swatch)} />
              <span className="min-w-0 flex-1 truncate text-muted-foreground">{partLabel(part.key)}</span>
              <span className="font-medium">{formatTokens(run.usage![part.key])}</span>
            </span>
          ) : null,
        )}
    </div>
  );
}

// ─── Cost flow ─────────────────────────────────────────────────────────────

// Room each label needs (name over figures), and the flow's floor height.
const FLOW_ROW_PX = 38;
const FLOW_MIN_HEIGHT = 72;
const FLOW_GAP_PX = 8;
// Models past this many share an "Others" node, like agents past the series.
const FLOW_MAX_MODELS = SERIES_COLORS.length;

function foldShares(shares: readonly CostShare[], keep: (key: string) => boolean): CostShare[] {
  const kept = shares.filter((s) => keep(s.key) && s.cost > 0);
  const rest = shares.filter((s) => !keep(s.key)).reduce((sum, s) => sum + s.cost, 0);
  return rest > 0 ? [...kept, { key: OTHERS_KEY, cost: rest }] : kept;
}

// Where the money went, as a flow: who spent it on the left, on which model on
// the right, a ribbon per pair as thick as its spend. It answers what two
// separate splits could not — which agent's spend went to which model, e.g.
// one agent switching models mid-issue.
function CostFlow({ timeline }: { timeline: RunTimeline }) {
  const { t } = useT("issues");
  const { getActorName } = useActorName();
  const percent = usePercent();
  const gradientId = useId();
  const [focus, setFocus] = useState<{ side: "agent" | "model"; key: string } | null>(null);
  const colors = useMemo(() => agentColorMap(timeline), [timeline]);

  const flow = useMemo(() => {
    const topModels = new Set(timeline.costByModel.slice(0, FLOW_MAX_MODELS).map((s) => s.key));
    const agentKey = (id: string) => (colors.get(id) != null ? id : OTHERS_KEY);
    const modelKey = (model: string) => (topModels.has(model) ? model : OTHERS_KEY);
    const agents = foldShares(timeline.costByAgent, (key) => colors.get(key) != null);
    const models = foldShares(timeline.costByModel, (key) => topModels.has(key));
    const pairs = new Map<string, { from: string; to: string; cost: number }>();
    for (const pair of timeline.costByAgentModel) {
      const from = agentKey(pair.agentId);
      const to = modelKey(pair.model);
      const key = `${from}\u0000${to}`;
      const link = pairs.get(key);
      if (link) link.cost += pair.cost;
      else pairs.set(key, { from, to, cost: pair.cost });
    }
    const height = Math.max(FLOW_MIN_HEIGHT, Math.max(agents.length, models.length) * FLOW_ROW_PX);
    const layout = layoutCostFlow(agents, models, [...pairs.values()], height, FLOW_GAP_PX);
    return {
      height,
      ...layout,
      leftLabels: spreadLabels(layout.left.map((n) => n.y + n.h / 2), FLOW_ROW_PX, height),
      rightLabels: spreadLabels(layout.right.map((n) => n.y + n.h / 2), FLOW_ROW_PX, height),
    };
  }, [timeline, colors]);

  if (flow.left.length === 0) return null;

  const colorOf = (agentKey: string) => colors.get(agentKey) ?? "var(--chart-bar-deep)";
  const agentName = (key: string) => (key === OTHERS_KEY ? t(($) => $.runs_timeline.others) : getActorName("agent", key));
  const modelName = (key: string) => (key === OTHERS_KEY ? t(($) => $.runs_timeline.others) : shortModelName(key));
  const lit = (side: "agent" | "model", key: string) => !focus || (focus.side === side && focus.key === key);
  const linkLit = (link: FlowLink) => !focus || (focus.side === "agent" ? link.from === focus.key : link.to === focus.key);
  const figures = (cost: number) => `${formatUsd(cost)} · ${percent(timeline.totalCost > 0 ? cost / timeline.totalCost : 0)}`;

  return (
    <div className="border-b px-6 pt-3 pb-4" onPointerLeave={() => setFocus(null)}>
      <div aria-hidden className="mb-2 flex justify-between text-micro font-medium uppercase tracking-wider text-muted-foreground">
        <span>{t(($) => $.runs_timeline.by_agent)}</span>
        <span>{t(($) => $.runs_timeline.by_model)}</span>
      </div>
      <div className="flex" style={{ height: flow.height }}>
        <section aria-label={t(($) => $.runs_timeline.by_agent)} className="relative w-40 shrink-0">
          {flow.left.map((node, i) => (
            <div
              key={node.key}
              data-flow-node={node.key}
              onPointerEnter={() => setFocus({ side: "agent", key: node.key })}
              className={cn("absolute right-3 left-0 -translate-y-1/2 text-right transition-opacity", !lit("agent", node.key) && "opacity-40")}
              style={{ top: flow.leftLabels[i] }}
            >
              <div className="truncate text-caption font-medium">{agentName(node.key)}</div>
              <div className="truncate text-micro tabular-nums text-muted-foreground">{figures(node.cost)}</div>
            </div>
          ))}
        </section>
        <div aria-hidden className="relative min-w-0 flex-1">
          {flow.left.map((node) => (
            <span
              key={node.key}
              className={cn("absolute left-0 w-2 rounded-xs chart-mark transition-opacity", !lit("agent", node.key) && "opacity-40")}
              style={{ top: node.y, height: Math.max(2, node.h), ...markStyle(colorOf(node.key)) }}
            />
          ))}
          <svg
            className="absolute inset-y-0 right-2.5 left-2.5 h-full w-[calc(100%-1.25rem)] overflow-visible"
            viewBox={`0 0 100 ${flow.height}`}
            preserveAspectRatio="none"
          >
            <defs>
              {flow.links.map((link, i) => (
                <linearGradient key={i} id={`${gradientId}-${i}`} x1="0" x2="1" y1="0" y2="0">
                  <stop offset="0" style={{ stopColor: colorOf(link.from), stopOpacity: 0.45 }} />
                  <stop offset="1" style={{ stopColor: colorOf(link.from), stopOpacity: 0.16 }} />
                </linearGradient>
              ))}
            </defs>
            {flow.links.map((link, i) => (
              <path
                key={i}
                d={`M0 ${link.y0} C50 ${link.y0} 50 ${link.y1} 100 ${link.y1} L100 ${link.y1 + link.h} C50 ${link.y1 + link.h} 50 ${link.y0 + link.h} 0 ${link.y0 + link.h} Z`}
                fill={`url(#${CSS.escape(gradientId)}-${i})`}
                className="transition-opacity"
                opacity={linkLit(link) ? 1 : 0.25}
                onPointerEnter={() => setFocus({ side: "agent", key: link.from })}
              />
            ))}
          </svg>
          {flow.right.map((node) => (
            <span
              key={node.key}
              className={cn("absolute right-0 w-2 rounded-xs chart-mark-neutral transition-opacity", !lit("model", node.key) && "opacity-40")}
              style={{ top: node.y, height: Math.max(2, node.h) }}
            />
          ))}
        </div>
        <section aria-label={t(($) => $.runs_timeline.by_model)} className="relative w-44 shrink-0">
          {flow.right.map((node, i) => (
            <div
              key={node.key}
              data-flow-node={node.key}
              title={node.key === OTHERS_KEY ? undefined : node.key}
              onPointerEnter={() => setFocus({ side: "model", key: node.key })}
              className={cn("absolute right-0 left-3 -translate-y-1/2 transition-opacity", !lit("model", node.key) && "opacity-40")}
              style={{ top: flow.rightLabels[i] }}
            >
              <div className="truncate text-caption font-medium">{modelName(node.key)}</div>
              <div className="truncate text-micro tabular-nums text-muted-foreground">{figures(node.cost)}</div>
            </div>
          ))}
        </section>
      </div>
    </div>
  );
}

// ─── List ──────────────────────────────────────────────────────────────────

// Column widths shared by the header and every row.
const COL = {
  time: "w-10 shrink-0",
  attribution: "w-4 shrink-0",
  arrow: "w-3 shrink-0",
  agent: "w-24 shrink-0",
  model: "w-24 shrink-0",
  status: "w-20 shrink-0",
  tokens: "w-14 shrink-0 text-right",
  cost: "w-14 shrink-0 text-right",
  actions: "w-14 shrink-0",
  toggle: "w-6 shrink-0",
} as const;

function RunDayList({ timeline, issueId }: { timeline: RunTimeline; issueId: string }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const groups = useMemo(() => groupRunsByDay(timeline.runs), [timeline.runs]);

  const dayLabel = useMemo(() => {
    const fmt = new Intl.DateTimeFormat(locale, { weekday: "short", month: "short", day: "numeric" });
    const today = new Date();
    today.setHours(0, 0, 0, 0);
    const yesterday = new Date(today);
    yesterday.setDate(today.getDate() - 1);
    return (dayMs: number) => {
      if (dayMs === today.getTime()) return t(($) => $.runs_timeline.day_today);
      if (dayMs === yesterday.getTime()) return t(($) => $.runs_timeline.day_yesterday);
      return fmt.format(dayMs);
    };
  }, [locale, t]);

  return (
    <div>
      <div
        aria-hidden
        className="sticky top-0 z-10 flex h-8 items-center gap-2 border-b bg-popover text-micro font-medium text-muted-foreground"
      >
        <span className={COL.time}>{t(($) => $.runs_timeline.col_time)}</span>
        <span className={COL.attribution} />
        <span className="min-w-0 flex-1">{t(($) => $.runs_timeline.col_trigger)}</span>
        <span className={COL.arrow} />
        <span className={COL.agent}>{t(($) => $.runs_timeline.col_agent)}</span>
        <span className={COL.model}>{t(($) => $.runs_timeline.col_model)}</span>
        <span className={COL.status}>{t(($) => $.runs_timeline.col_duration)}</span>
        <span className={COL.tokens}>{t(($) => $.runs_timeline.col_tokens)}</span>
        <span className={COL.cost}>{t(($) => $.runs_timeline.col_cost)}</span>
        <span className={COL.actions} />
        <span className={COL.toggle} />
      </div>
      {groups.map((group) => {
        const priced = group.runs.some((r) => r.usage);
        return (
          <section key={group.dayMs} aria-label={dayLabel(group.dayMs)}>
            <div className="flex items-baseline gap-2 border-b pt-4 pb-1.5 text-micro text-muted-foreground">
              <span className="font-medium uppercase tracking-wider text-foreground">
                {dayLabel(group.dayMs)}
              </span>
              <span className="ml-auto tabular-nums">
                {[
                  t(($) => $.runs_timeline.day_runs, { count: group.runs.length }),
                  group.agentMs > 0 ? formatDuration(group.agentMs / 1000, UNDER_A_SECOND) : null,
                  priced ? t(($) => $.runs_timeline.tokens_count, { tokens: formatTokens(group.tokens) }) : null,
                  priced ? formatUsd(group.cost) : null,
                ]
                  .filter(Boolean)
                  .join(" · ")}
              </span>
            </div>
            {group.runs.map((run) => (
              <RunListRow key={run.task.id} run={run} issueId={issueId} />
            ))}
          </section>
        );
      })}
    </div>
  );
}

function useStatusReason(task: AgentTask): string | null {
  const { t: tAgents } = useT("agents");
  // Same localized reason the sidebar rows show — never the raw `task.error`,
  // which is operator-facing English (#7411).
  const reason =
    task.status === "failed"
      ? failureReasonLabel(task.failure_reason, tAgents)
      : cancelReasonLabel(task, tAgents);
  const cancelledBy = cancellationActorLabel(task, tAgents);
  return [cancelledBy, reason].filter(Boolean).join(" · ") || null;
}

function RunListRow({ run, issueId }: { run: TimelineRun; issueId: string }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const [expanded, setExpanded] = useState(false);
  const trigger = useTriggerText(run.task);
  const statusLabel = useStatusLabel(run.task.status);
  const reason = useStatusReason(run.task);
  const task = run.task;
  const time = new Intl.DateTimeFormat(locale, {
    hour: "2-digit",
    minute: "2-digit",
    hour12: false,
  }).format(run.startMs);
  // Comment-triggered runs quote what the person said; structural triggers
  // (assignment, autopilot, retry labels) read as plain labels, and so do
  // wakeup runs, whose comment id points at the thread the rule lives in.
  const quoted = !task.wakeup_id && !!task.trigger_summary && !!task.trigger_comment_id;
  const agentName = getActorName("agent", task.agent_id);
  const label = quoted ? t(($) => $.runs_timeline.quoted, { text: trigger }) : trigger;
  const model = run.usage?.models[0];
  const detailsId = `run-details-${task.id}`;

  return (
    <div className={cn("border-b", expanded && "bg-accent/40")}>
      <div className="group/run-row flex h-9 items-center gap-2 text-caption transition-colors hover:bg-accent/40">
        <span className={cn(COL.time, "font-mono text-micro tabular-nums text-muted-foreground")}>
          {time}
        </span>
        <span className={cn(COL.attribution, "flex justify-center")}>
          <AttributionBadge attribution={task.attribution} variant="avatar" />
        </span>
        {/* One line keeps the list scannable; the full text is in the native
            tooltip and, whole and wrapped, at the top of the transcript. */}
        <RunTriggerLabel task={task} fallback={label}>
          {(text) => (
            <TruncatedTooltip content={text}>
              <span
                className={cn(
                  "min-w-0 flex-1 truncate text-label",
                  run.usage || run.active ? "text-foreground" : "text-muted-foreground",
                )}
              >
                {text}
              </span>
            </TruncatedTooltip>
          )}
        </RunTriggerLabel>
        <ArrowRight aria-hidden className={cn(COL.arrow, "size-3 text-faint-foreground")} />
        <span className={cn(COL.agent, "flex items-center gap-1.5")}>
          <ActorAvatar actorType="agent" actorId={task.agent_id} size="xs" enableHoverCard />
          <span className="truncate">{agentName}</span>
        </span>
        <span className={cn(COL.model, "min-w-0")}>
          {model ? (
            <span
              title={run.usage!.models.join(", ")}
              className="inline-block max-w-full truncate rounded-xs bg-muted px-1.5 py-0.5 align-middle text-micro text-muted-foreground"
            >
              {shortModelName(model)}
            </span>
          ) : (
            <span className="text-faint-foreground">—</span>
          )}
        </span>
        <span className={cn(COL.status, "flex items-center gap-1")} title={reason ?? statusLabel}>
          {run.active ? (
            <span className="flex items-center gap-1 text-info">
              <span className="size-1.5 rounded-full bg-info" />
              {statusLabel}
            </span>
          ) : task.status === "failed" ? (
            <span className="flex min-w-0 items-center gap-1 text-destructive">
              <XCircle aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{statusLabel}</span>
            </span>
          ) : task.status === "cancelled" ? (
            <span className="flex min-w-0 items-center gap-1 text-muted-foreground">
              <Ban aria-hidden className="size-3.5 shrink-0" />
              <span className="truncate">{statusLabel}</span>
            </span>
          ) : (
            <>
              <span className="tabular-nums text-muted-foreground">
                {run.durationMs != null ? formatDuration(run.durationMs / 1000, UNDER_A_SECOND) : "—"}
              </span>
              <span className="sr-only">{statusLabel}</span>
            </>
          )}
        </span>
        {run.usage ? (
          <>
            <span className={cn(COL.tokens, "tabular-nums text-muted-foreground")}>
              {formatTokens(run.usage.tokens)}
            </span>
            <span className={cn(COL.cost, "font-medium tabular-nums")}>{formatUsd(run.usage.cost)}</span>
          </>
        ) : (
          // No figure is not zero: a run without usage data was not free.
          <>
            <span className={cn(COL.tokens, "text-faint-foreground")}>—</span>
            <span className={cn(COL.cost, "text-faint-foreground")} title={t(($) => $.runs_timeline.no_usage)}>
              —
            </span>
          </>
        )}
        <span className={cn(COL.actions, "flex items-center justify-end gap-0.5 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/run-row:opacity-100 [@media(hover:hover)]:focus-within:opacity-100")}>
          <TranscriptButton
            task={task}
            agentName={agentName}
            isLive={task.status === "running"}
            title={t(($) => $.execution_log.transcript_tooltip)}
          />
          {canRetryRun(task) && <RetryRunButton task={task} issueId={issueId} />}
        </span>
        <span className={cn(COL.toggle, "flex justify-end")}>
          <DeferredTooltip
            content={t(($) => $.runs_timeline.details_toggle)}
            trigger={
              <button
                type="button"
                aria-expanded={expanded}
                aria-controls={detailsId}
                aria-label={t(($) => $.runs_timeline.details_toggle)}
                onClick={() => setExpanded((v) => !v)}
                className="flex size-6 items-center justify-center rounded-xs text-muted-foreground transition-colors outline-none hover:bg-accent hover:text-foreground focus-visible:ring-2 focus-visible:ring-ring/50"
              >
                <ChevronRight aria-hidden className={cn("size-3.5 transition-transform", expanded && "rotate-90")} />
              </button>
            }
          />
        </span>
      </div>
      {expanded && <RunDetails id={detailsId} run={run} reason={reason} />}
    </div>
  );
}

function RunDetails({ id, run, reason }: { id: string; run: TimelineRun; reason: string | null }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const partLabel = useCostPartLabel();
  const percent = usePercent();
  const statusLabel = useStatusLabel(run.task.status);
  const usage = run.usage;
  const breakdown = run.breakdown;
  const hit = usage ? cacheHitRate(usage) : null;
  const hm = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false });
  const tokenCount: Record<CostPartKey, number> = usage
    ? { input: usage.input, output: usage.output, cacheRead: usage.cacheRead, cacheWrite: usage.cacheWrite }
    : { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
  const statusTone =
    run.task.status === "failed"
      ? "text-destructive"
      : run.active
        ? "text-info"
        : undefined;

  return (
    <div id={id} className="grid grid-cols-1 gap-x-8 gap-y-4 px-3 pt-2 pb-4 pl-[4.5rem] text-caption md:grid-cols-[minmax(0,1fr)_16rem]">
      {usage && breakdown ? (
        <div className="min-w-0">
          <div className="mb-2 text-micro font-medium uppercase tracking-wider text-muted-foreground">
            {t(($) => $.runs_timeline.detail_breakdown)}
          </div>
          <div aria-hidden className="mb-2.5 flex h-1.5 gap-0.5 overflow-hidden rounded-full">
            {COST_PARTS.map((part) =>
              breakdown[part.key] > 0 ? (
                <span
                  key={part.key}
                  className={part.swatch}
                  style={shareStyle(breakdown[part.key], sumOf(COST_PARTS.map((p) => breakdown[p.key])))}
                />
              ) : null,
            )}
          </div>
          <div className="grid grid-cols-[auto_minmax(0,1fr)_auto_auto_auto] items-center gap-x-3 gap-y-1 tabular-nums">
            {COST_PARTS.map((part) => (
              <RunDetailPart
                key={part.key}
                swatch={part.swatch}
                label={partLabel(part.key)}
                tokens={tokenCount[part.key]}
                cost={breakdown[part.key]}
                share={usage.cost > 0 ? percent(breakdown[part.key] / usage.cost) : ""}
              />
            ))}
            <span />
            <span className="font-medium">{t(($) => $.runs_timeline.detail_total)}</span>
            <span className="text-right font-medium">{formatTokens(usage.tokens)}</span>
            <span className="w-14 text-right font-semibold">{formatUsd(usage.cost)}</span>
            <span />
          </div>
        </div>
      ) : (
        <p className="text-muted-foreground">{t(($) => $.runs_timeline.no_usage)}</p>
      )}
      <dl className="grid grid-cols-[auto_minmax(0,1fr)] content-start gap-x-3 gap-y-1">
        <dt className="text-muted-foreground">{t(($) => $.runs_timeline.detail_status)}</dt>
        <dd className={cn("min-w-0", statusTone)}>{[statusLabel, reason].filter(Boolean).join(" · ")}</dd>
        {usage && usage.models.length > 0 && (
          <>
            <dt className="text-muted-foreground">{t(($) => $.runs_timeline.detail_model)}</dt>
            <dd className="min-w-0 break-words">{usage.models.join(", ")}</dd>
          </>
        )}
        {usage && hit != null && (
          <>
            <dt className="text-muted-foreground">{t(($) => $.runs_timeline.detail_cache)}</dt>
            <dd className="tabular-nums">
              {t(($) => $.runs_timeline.cache_hit_value, {
                percent: percent(hit),
                amount: formatUsd(usage.cacheSavings),
              })}
            </dd>
          </>
        )}
        <dt className="text-muted-foreground">{t(($) => $.runs_timeline.detail_time)}</dt>
        <dd className="tabular-nums">
          {run.active ? hm.format(run.startMs) : `${hm.format(run.startMs)}–${hm.format(run.endMs)}`}
        </dd>
        {run.task.branch_name && (
          <>
            <dt className="text-muted-foreground">{t(($) => $.runs_timeline.detail_branch)}</dt>
            <TruncatedTooltip content={run.task.branch_name}>
              <dd className="min-w-0 truncate font-mono text-micro">
                {run.task.branch_name}
              </dd>
            </TruncatedTooltip>
          </>
        )}
      </dl>
    </div>
  );
}

function RunDetailPart({
  swatch,
  label,
  tokens,
  cost,
  share,
}: {
  swatch: string;
  label: string;
  tokens: number;
  cost: number;
  share: string;
}) {
  return (
    <>
      <span aria-hidden className={cn("size-2 rounded-xs", swatch)} />
      <span className="truncate text-muted-foreground">{label}</span>
      <span className="text-right">{formatTokens(tokens)}</span>
      <span className="w-14 text-right font-medium">{formatUsd(cost)}</span>
      <span className="w-10 text-right text-faint-foreground">{share}</span>
    </>
  );
}
