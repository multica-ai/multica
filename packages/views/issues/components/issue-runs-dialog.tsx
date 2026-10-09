"use client";

import { useMemo, useRef, useState } from "react";
import { ArrowRight, Ban, XCircle } from "lucide-react";
import type { AgentTask } from "@multica/core/types";
import { cn } from "@multica/ui/lib/utils";
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from "@multica/ui/components/ui/dialog";
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from "@multica/ui/components/ui/popover";
import { useActorName } from "@multica/core/workspace/hooks";
import { useCustomPricingStore } from "@multica/core/runtimes/custom-pricing-store";
import { ActorAvatar } from "../../common/actor-avatar";
import { TranscriptButton } from "../../common/task-transcript";
import { cancellationActorLabel, cancelReasonLabel, failureReasonLabel } from "../../agents/components/tabs/task-failure";
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
  cumulativeCostAt,
  groupRunsByDay,
  idleSpanAround,
  niceTicks,
  runIndexAt,
  stepCurvePath,
  timeTicks,
  type RunTimeline,
  type TimelineRun,
} from "./issue-run-timeline";
import { canRetryRun, RetryRunButton } from "./retry-run-button";
import { useStatusLabel, useTriggerText } from "./task-run-labels";
import { WakeupRunLabel } from "./wakeup-source-chip";

// The issue's runs laid out in time — the surface the execution log's header
// and spend strip open.
//
// The sidebar answers "what is running and what just ran"; this answers "how
// did this issue get here": when each run happened, how long it took, who asked
// for it, and which ones moved the total. The chart on top is the shape of the
// issue (a cumulative cost curve over per-agent run lanes on one time axis);
// the list below is the same runs as rows, newest first — it is also the
// chart's accessible, exact-value view.
//
// Every figure comes from the same `summarizeTaskUsage` helpers the sidebar
// uses, so the total here can never disagree with the total that opened it.

// Cost categories in the order and colours the runtime usage charts stack them
// (`costStackConfig` in runtimes/components/charts/daily-cost-chart.tsx), so a
// run's cost bar reads the same as a day's bar on the usage page.
const COST_PARTS = [
  { key: "input", swatch: "bg-chart-1" },
  { key: "output", swatch: "bg-chart-2" },
  { key: "cacheRead", swatch: "bg-chart-4" },
  { key: "cacheWrite", swatch: "bg-chart-3" },
] as const satisfies readonly { key: keyof CostBreakdown; swatch: string }[];

type CostPartKey = (typeof COST_PARTS)[number]["key"];

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
  // changes or the dialog reopens rather than ticking: the bars move by
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
            <RunTimelineChart timeline={timeline} />
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
    <div className="min-w-0">
      <div className="text-micro font-medium uppercase tracking-wider text-muted-foreground">
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
          {timeline.pricedCount > 0 ? formatUsd(timeline.totalCost) : "—"}
        </span>
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_agent_time)}>
        <span className="text-title-sm font-medium">
          {formatDuration(timeline.agentMs / 1000, UNDER_A_SECOND)}
        </span>
      </Stat>
      <Stat label={t(($) => $.runs_timeline.stat_elapsed)}>
        <span className="text-title-sm font-medium">
          {formatDuration(timeline.elapsedMs / 1000, UNDER_A_SECOND)}
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
      <div className="ml-auto flex items-center gap-4 pb-0.5 text-caption text-muted-foreground">
        {timeline.pricedCount > 0 && (
          <span className="flex items-center gap-1.5">
            <span aria-hidden className="h-0.5 w-3.5 rounded-full bg-chart-1" />
            {t(($) => $.runs_timeline.legend_cumulative)}
          </span>
        )}
        <span className="flex items-center gap-1.5">
          <span aria-hidden className="h-2 w-3 rounded-xs bg-chart-2" />
          {t(($) => $.runs_timeline.legend_run)}
        </span>
      </div>
    </div>
  );
}

// ─── Chart ─────────────────────────────────────────────────────────────────

const PLOT_HEIGHT = 112;
// How far the pointer can miss a bar and still be on it: a run a few seconds
// long draws as a 3px sliver, which nobody lands on exactly.
const HOVER_SLOP_PX = 8;
// The hover card's `max-w-72` plus its gap from the crosshair. With less room
// than this to the right of the crosshair, the card opens on its left.
const HOVER_CARD_ROOM_PX = 298;
// Half the widest pointer-time tag ("Sep 27, 14:44"): nearer the plot's edge
// than this, the tag aligns to the edge instead of centring on the crosshair,
// and tick labels this close to it step aside.
const TIME_TAG_HALF_PX = 48;

export function runBarTone(run: TimelineRun, isPeak: boolean): string {
  if (run.active) return "bg-info animate-pulse";
  if (run.task.status === "failed") return "bg-destructive";
  if (run.task.status === "cancelled") return "bg-faint-foreground";
  return isPeak ? "bg-chart-1" : "bg-chart-2";
}

interface ChartHover {
  /** The moment under the pointer, clamped to the stretch that holds runs. */
  t: number;
  /** The lane the pointer is over, if any. */
  lane?: string;
  /** The plot's rendered width, which turns pixels into time. */
  width: number;
}

function RunTimelineChart({ timeline }: { timeline: RunTimeline }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const [d0, d1] = timeline.domain;
  // Without any usage there is no curve to draw; the lanes still show when
  // each run happened.
  const plotHeight = timeline.pricedCount > 0 ? PLOT_HEIGHT : 0;
  const xPct = (ms: number) => ((ms - d0) / (d1 - d0)) * 100;

  const yTicks = niceTicks(timeline.totalCost);
  const yMax = yTicks[yTicks.length - 1] ?? 1;
  const yPct = (cost: number) => (1 - cost / yMax) * 100;

  const ticks = timeTicks(timeline.domain);
  const multiDay = d1 - d0 > 36 * 60 * 60 * 1000;
  const [tickFormat, pointerFormat] = useMemo(() => {
    const day = new Intl.DateTimeFormat(locale, { weekday: "short", month: "short", day: "numeric" });
    const hour = new Intl.DateTimeFormat(locale, { hour: "2-digit", minute: "2-digit", hour12: false });
    const dayHour = new Intl.DateTimeFormat(locale, {
      month: "short",
      day: "numeric",
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
    return [
      (tick: (typeof ticks)[number]) => (tick.kind === "day" ? day.format(tick.t) : hour.format(tick.t)),
      (ms: number) => (multiDay ? dayHour.format(ms) : hour.format(ms)),
    ] as const;
  }, [locale, multiDay]);

  // Step curve in a 1000×100 box stretched over the plot; the stroke keeps its
  // 2px via non-scaling-stroke however wide the dialog is.
  const steps = timeline.cumulative;
  const { line, area } = stepCurvePath(steps, timeline.domain, yMax);
  const last = steps[steps.length - 1];

  // Label the one run that moved the curve most. Left of its step the curve is
  // lower (it only ever rises), so a label ending at the step's top-left
  // corner never sits on the line. Near the left edge there is no room for
  // that; right of the step the curve can still rise into anything above it,
  // so the label drops just below the line instead, into the area wash.
  const peak = timeline.peak;
  const peakStep = peak ? steps.find((s) => s.t === peak.endMs) : undefined;
  const peakX = peakStep ? xPct(peakStep.t) : 0;

  // Hover layer. The crosshair is the pointer: it follows it across the whole
  // chart row — lane labels and y scale included, so drifting a few pixels
  // off the plot does not drop the hover — clamped to the stretch that holds
  // runs. Whatever is under it answers: the run whose bar spans that moment,
  // or between runs, the quiet stretch and what the curve reads there. It used
  // to snap to the nearest run's end instead, which moved it hundreds of
  // pixels for a few pixels of pointer travel (MUL-7780). The list below
  // carries the same figures for keyboard and screen-reader users.
  const plotRef = useRef<HTMLDivElement>(null);
  const [hover, setHover] = useState<ChartHover | null>(null);
  const trackPointer = (event: React.PointerEvent<HTMLDivElement>) => {
    const rect = plotRef.current?.getBoundingClientRect();
    if (!rect || rect.width <= 0) return;
    const [e0, e1] = timeline.extent;
    const at = d0 + ((event.clientX - rect.left) / rect.width) * (d1 - d0);
    // Over a lane, only that lane's runs are candidates — the pointer is on a
    // specific agent's row. Over the curve, any agent's run can answer.
    const lane = (event.target as Element).closest?.("[data-lane]")?.getAttribute("data-lane");
    setHover({ t: Math.min(Math.max(at, e0), e1), lane: lane ?? undefined, width: rect.width });
  };

  const hoverIndex = hover
    ? runIndexAt(timeline.runs, hover.t, (HOVER_SLOP_PX / hover.width) * (d1 - d0), hover.lane)
    : -1;
  const hovered = hoverIndex >= 0 ? timeline.runs[hoverIndex] : undefined;
  const hoverX = hover ? xPct(hover.t) : 0;
  const hoverPx = hover ? (hoverX / 100) * hover.width : 0;
  // The point the card is about: the hovered run's step on the curve, or
  // between runs, the curve's reading under the crosshair. A run that has not
  // reported usage has no step to point at.
  const dot =
    !hover || plotHeight === 0
      ? null
      : hovered
        ? hovered.usage
          ? { x: xPct(hovered.endMs), cost: hovered.costSoFar }
          : null
        : { x: hoverX, cost: cumulativeCostAt(steps, hover.t) };
  // The card sits in whichever half of the plot its dot is not in, so it never
  // covers the point it describes.
  const cardTop = dot && yPct(dot.cost) < 50 ? Math.round(plotHeight * 0.4) : 0;
  const cardOnLeft = hover ? hoverPx + HOVER_CARD_ROOM_PX > hover.width : false;
  const tagTransform = !hover
    ? undefined
    : hoverPx < TIME_TAG_HALF_PX
      ? "none"
      : hoverPx > hover.width - TIME_TAG_HALF_PX
        ? "translateX(-100%)"
        : "translateX(-50%)";

  return (
    <div
      role="img"
      aria-label={t(($) => $.runs_timeline.chart_aria, {
        count: timeline.runs.length,
        cost: formatUsd(timeline.totalCost),
      })}
      className="mt-5 flex gap-3 border-b px-6 pb-3"
      onPointerMove={trackPointer}
      onPointerDown={trackPointer}
      onPointerLeave={() => setHover(null)}
    >
      {/* Lane labels, aligned with the lanes in the plot column. */}
      <div className="w-24 shrink-0" aria-hidden>
        <div style={{ height: plotHeight }} />
        <div className={cn(plotHeight > 0 && "mt-[9px]")}>
          {timeline.lanes.map((lane) => (
            <div key={lane.agentId} className="flex h-5 min-w-0 items-center gap-1.5">
              <ActorAvatar actorType="agent" actorId={lane.agentId} size="xs" />
              <span className="truncate text-micro text-muted-foreground">
                {getActorName("agent", lane.agentId)}
              </span>
            </div>
          ))}
        </div>
      </div>

      <div ref={plotRef} className="relative min-w-0 flex-1">
        {/* Time gridlines run through the curve and the lanes alike. */}
        {ticks.map((tick) => (
          <span
            key={tick.t}
            aria-hidden
            className="absolute top-0 bottom-5 w-px bg-border"
            style={{ left: `${xPct(tick.t)}%` }}
          />
        ))}

        <div className="relative" style={{ height: plotHeight }} aria-hidden>
          {yTicks.map((v) => (
            <span
              key={v}
              className="absolute inset-x-0 h-px bg-border"
              style={{ top: `${yPct(v)}%` }}
            />
          ))}
          {steps.length > 0 && (
            <svg
              className="absolute inset-0 size-full overflow-visible"
              viewBox="0 0 1000 100"
              preserveAspectRatio="none"
            >
              <path d={area} fill="var(--chart-1)" fillOpacity={0.1} />
              <path
                d={line}
                fill="none"
                stroke="var(--chart-1)"
                strokeWidth={2}
                strokeLinejoin="round"
                vectorEffect="non-scaling-stroke"
              />
            </svg>
          )}
          {last && (
            <span
              className="absolute size-2 -translate-x-1/2 -translate-y-1/2 rounded-full bg-chart-1 ring-2 ring-popover"
              style={{ left: `${xPct(last.t)}%`, top: `${yPct(last.cost)}%` }}
            />
          )}
          {dot && (
            <span
              className="pointer-events-none absolute z-10 size-2.5 -translate-x-1/2 -translate-y-1/2 rounded-full bg-chart-1 ring-2 ring-popover"
              style={{ left: `${dot.x}%`, top: `${yPct(dot.cost)}%` }}
            />
          )}
          {/* Fades rather than unmounts under the hover card, so entering and
              leaving the chart does not blink it. */}
          {peak && peakStep && (
            <PeakLabel
              run={peak}
              className={cn("transition-opacity duration-150", hover && "opacity-0")}
              style={{
                left: `${peakX}%`,
                top: `${yPct(peakStep.cost)}%`,
                transform:
                  peakX >= 30
                    ? "translate(calc(-100% - 6px), calc(-100% - 2px))"
                    : "translate(6px, 4px)",
              }}
            />
          )}
        </div>

        {/* Rows, not tracks, carry `data-lane`, and they touch: moving from one
            lane to the next never crosses a gap that belongs to no lane, which
            would briefly hand the hover to every agent's runs. */}
        <div className={cn(plotHeight > 0 && "mt-[9px]")}>
          {timeline.lanes.map((lane) => (
            <div key={lane.agentId} data-lane={lane.agentId} className="py-[3px]">
              <div className="relative h-3.5 rounded-xs bg-muted/60">
                {lane.runs.map((run) => (
                  <span
                    key={run.task.id}
                    className={cn(
                      "absolute inset-y-0.5 min-w-[3px] rounded-xs transition-opacity",
                      runBarTone(run, run === peak),
                      hovered && run !== hovered && "opacity-35",
                    )}
                    style={{
                      left: `${xPct(run.startMs)}%`,
                      width: `${xPct(run.endMs) - xPct(run.startMs)}%`,
                    }}
                  />
                ))}
              </div>
            </div>
          ))}
        </div>

        {hover && (
          <>
            <span
              aria-hidden
              className="pointer-events-none absolute top-0 bottom-5 w-px bg-foreground/30"
              style={{ left: `${hoverX}%` }}
            />
            {/* Beside the crosshair, on whichever side has room. */}
            <div
              aria-hidden
              data-hover-card
              className="pointer-events-none absolute z-20 w-max max-w-72 rounded-lg border bg-popover px-2.5 py-1.5 text-caption text-popover-foreground shadow-[var(--menu-shadow)]"
              style={{
                left: `${hoverX}%`,
                top: cardTop,
                transform: cardOnLeft ? "translateX(calc(-100% - 10px))" : "translateX(10px)",
              }}
            >
              {hovered ? (
                <RunHoverCard run={hovered} />
              ) : (
                <IdleHoverCard
                  {...idleSpanAround(timeline.runs, hover.t, hover.lane)}
                  total={timeline.pricedCount > 0 ? cumulativeCostAt(steps, hover.t) : null}
                />
              )}
            </div>
          </>
        )}

        <div className="relative mt-px h-5" aria-hidden>
          {ticks.map((tick) => {
            const x = xPct(tick.t);
            // The pointer's own time tag takes the axis where it stands.
            if (hover && Math.abs(((x - hoverX) / 100) * hover.width) < TIME_TAG_HALF_PX) return null;
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
          {hover && (
            <span
              className="pointer-events-none absolute top-0.5 z-20 whitespace-nowrap rounded-xs bg-foreground px-1 text-micro font-medium tabular-nums text-background"
              style={{ left: `${hoverX}%`, transform: tagTransform }}
            >
              {pointerFormat(hover.t)}
            </span>
          )}
        </div>
      </div>

      {/* Y scale on the right, where the curve ends; the end value is the
          one figure labelled in full. */}
      <div className="relative w-12 shrink-0 text-micro tabular-nums" style={{ height: plotHeight }} aria-hidden>
        {yTicks.map((v) =>
          last && Math.abs(yPct(v) - yPct(last.cost)) < 12 ? null : (
            <span
              key={v}
              className="absolute left-1 -translate-y-1/2 text-muted-foreground"
              style={{ top: `${yPct(v)}%` }}
            >
              {formatTick(v)}
            </span>
          ),
        )}
        {last && (
          <span
            className="absolute left-1 -translate-y-1/2 font-medium text-foreground"
            style={{ top: `${yPct(last.cost)}%` }}
          >
            {formatUsd(last.cost)}
          </span>
        )}
      </div>
    </div>
  );
}

// Axis steps are round numbers; "$50.00" would print noise the curve's end
// label, which keeps full precision, does not need.
function formatTick(v: number): string {
  return Number.isInteger(v) ? `$${v}` : formatUsd(v);
}

/**
 * A run's trigger, with a wakeup run naming its rule's condition ("Wakeup ·
 * When a linked PR's CI finishes") the way the execution log's rows do. The
 * rule lookup mounts only for wakeup runs.
 */
export function RunTriggerLabel({
  task,
  fallback,
  children,
}: {
  task: AgentTask;
  fallback: string;
  children: (label: string) => React.ReactNode;
}) {
  if (!task.wakeup_id) return <>{children(fallback)}</>;
  return <WakeupRunLabel task={task} fallback={fallback} render={children} />;
}

function PeakLabel({
  run,
  className,
  style,
}: {
  run: TimelineRun;
  className?: string;
  style: React.CSSProperties;
}) {
  const trigger = useTriggerText(run.task);
  return (
    <span
      className={cn("absolute flex max-w-60 items-baseline gap-1 whitespace-nowrap text-micro", className)}
      style={style}
    >
      <span className="font-medium text-foreground">+{formatUsd(run.usage?.cost ?? 0)}</span>
      <RunTriggerLabel task={run.task} fallback={trigger}>
        {(label) => <span className="truncate text-muted-foreground">{label}</span>}
      </RunTriggerLabel>
    </span>
  );
}

function RunHoverCard({ run }: { run: TimelineRun }) {
  const { t } = useT("issues");
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
        <span className="font-medium">
          {run.usage ? formatUsd(run.usage.cost) : t(($) => $.runs_timeline.no_usage)}
        </span>
        <span className="text-muted-foreground">
          {" · "}
          {t(($) => $.runs_timeline.tooltip_total, { cost: formatUsd(run.costSoFar) })}
        </span>
      </span>
    </div>
  );
}

// Between runs: nothing ran here. Says so, with how long the quiet lasted and
// what the issue had spent by then — the curve's reading under the crosshair.
function IdleHoverCard({
  fromMs,
  toMs,
  total,
}: {
  fromMs: number | null;
  toMs: number | null;
  /** Null when no run reported usage — there is no total to quote. */
  total: number | null;
}) {
  const { t } = useT("issues");
  const locale = useLocale();
  const span = useMemo(() => {
    if (fromMs == null || toMs == null) return null;
    const sameDay = new Date(fromMs).toDateString() === new Date(toMs).toDateString();
    const fmt = new Intl.DateTimeFormat(locale, {
      ...(sameDay ? {} : { month: "short", day: "numeric" }),
      hour: "2-digit",
      minute: "2-digit",
      hour12: false,
    });
    return `${fmt.format(fromMs)} → ${fmt.format(toMs)}`;
  }, [fromMs, toMs, locale]);
  return (
    <div className="flex min-w-0 flex-col gap-1">
      <span className="font-medium">{t(($) => $.runs_timeline.hover_no_run)}</span>
      {span && fromMs != null && toMs != null && (
        <span className="truncate text-micro tabular-nums text-muted-foreground">
          {span} ·{" "}
          {t(($) => $.runs_timeline.hover_idle, {
            duration: formatDuration((toMs - fromMs) / 1000, UNDER_A_SECOND),
          })}
        </span>
      )}
      {total != null && (
        <span className="text-micro tabular-nums text-muted-foreground">
          {t(($) => $.runs_timeline.tooltip_total, { cost: formatUsd(total) })}
        </span>
      )}
    </div>
  );
}

// ─── List ──────────────────────────────────────────────────────────────────

function RunDayList({ timeline, issueId }: { timeline: RunTimeline; issueId: string }) {
  const { t } = useT("issues");
  const locale = useLocale();
  const partLabel = useCostPartLabel();
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
      {timeline.pricedCount > 0 && (
        <div className="flex justify-end gap-3 pt-3 text-micro text-muted-foreground">
          {COST_PARTS.map((part) => (
            <span key={part.key} className="flex items-center gap-1.5">
              <span aria-hidden className={cn("size-2 rounded-xs", part.swatch)} />
              {partLabel(part.key)}
            </span>
          ))}
        </div>
      )}
      {groups.map((group) => (
        <section key={group.dayMs} aria-label={dayLabel(group.dayMs)}>
          <div className="flex items-baseline gap-2 border-b pt-4 pb-1.5 text-micro text-muted-foreground">
            <span className="font-medium uppercase tracking-wider text-foreground">
              {dayLabel(group.dayMs)}
            </span>
            <span className="ml-auto tabular-nums">
              {[
                t(($) => $.runs_timeline.day_runs, { count: group.runs.length }),
                group.agentMs > 0 ? formatDuration(group.agentMs / 1000, UNDER_A_SECOND) : null,
                group.runs.some((r) => r.usage) ? formatUsd(group.cost) : null,
              ]
                .filter(Boolean)
                .join(" · ")}
            </span>
          </div>
          {group.runs.map((run) => (
            <RunListRow
              key={run.task.id}
              run={run}
              issueId={issueId}
              maxCost={timeline.maxRunCost}
            />
          ))}
        </section>
      ))}
    </div>
  );
}

function RunListRow({
  run,
  issueId,
  maxCost,
}: {
  run: TimelineRun;
  issueId: string;
  maxCost: number;
}) {
  const { t } = useT("issues");
  const { t: tAgents } = useT("agents");
  const locale = useLocale();
  const { getActorName } = useActorName();
  const trigger = useTriggerText(run.task);
  const statusLabel = useStatusLabel(run.task.status);
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
  // Same localized reason the sidebar rows hover with — never the raw
  // `task.error`, which is operator-facing English (#7411).
  const reason =
    task.status === "failed"
      ? failureReasonLabel(task.failure_reason, tAgents)
      : cancelReasonLabel(task, tAgents);
  const cancelledBy = cancellationActorLabel(task, tAgents);
  const statusTitle = [cancelledBy, reason].filter(Boolean).join(" · ") || statusLabel;
  const agentName = getActorName("agent", task.agent_id);
  const label = quoted ? t(($) => $.runs_timeline.quoted, { text: trigger }) : trigger;

  return (
    <div className="group/run-row flex h-9 items-center gap-2.5 border-b text-caption transition-colors hover:bg-accent/40">
      <span className="w-10 shrink-0 font-mono text-micro tabular-nums text-muted-foreground">
        {time}
      </span>
      <span className="flex w-4 shrink-0 justify-center">
        <AttributionBadge attribution={task.attribution} variant="avatar" />
      </span>
      {/* One line keeps the list scannable; the full text is in the native
          tooltip and, whole and wrapped, at the top of the transcript. */}
      <RunTriggerLabel task={task} fallback={label}>
        {(text) => (
          <span
            title={text}
            className={cn(
              "min-w-0 flex-1 truncate text-label",
              run.usage || run.active ? "text-foreground" : "text-muted-foreground",
            )}
          >
            {text}
          </span>
        )}
      </RunTriggerLabel>
      <ArrowRight aria-hidden className="size-3 shrink-0 text-faint-foreground" />
      <span className="flex w-28 shrink-0 items-center gap-1.5">
        <ActorAvatar actorType="agent" actorId={task.agent_id} size="xs" enableHoverCard />
        <span className="truncate">{agentName}</span>
      </span>
      <span className="flex w-24 shrink-0 items-center gap-1" title={statusTitle}>
        {run.active ? (
          <span className="flex items-center gap-1 text-info">
            <span className="size-1.5 rounded-full bg-info" />
            {statusLabel}
          </span>
        ) : task.status === "failed" ? (
          <span className="flex items-center gap-1 text-destructive">
            <XCircle aria-hidden className="size-3.5 shrink-0" />
            {statusLabel}
          </span>
        ) : task.status === "cancelled" ? (
          <span className="flex items-center gap-1 text-muted-foreground">
            <Ban aria-hidden className="size-3.5 shrink-0" />
            {statusLabel}
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
      <span className="flex w-36 shrink-0 items-center justify-end gap-2">
        {run.usage && run.breakdown ? (
          <CostCell run={run} maxCost={maxCost} />
        ) : (
          // No figure is not zero: a run without usage data was not free.
          <span className="text-faint-foreground" title={t(($) => $.runs_timeline.no_usage)}>
            —
          </span>
        )}
      </span>
      <span className="flex w-14 shrink-0 items-center justify-end gap-0.5 [@media(hover:hover)]:opacity-0 [@media(hover:hover)]:group-hover/run-row:opacity-100 [@media(hover:hover)]:focus-within:opacity-100">
        <TranscriptButton
          task={task}
          agentName={agentName}
          isLive={task.status === "running"}
          title={t(($) => $.execution_log.transcript_tooltip)}
        />
        {canRetryRun(task) && <RetryRunButton task={task} issueId={issueId} />}
      </span>
    </div>
  );
}

function CostCell({ run, maxCost }: { run: TimelineRun; maxCost: number }) {
  const { t } = useT("issues");
  const partLabel = useCostPartLabel();
  const usage = run.usage!;
  const breakdown = run.breakdown!;
  const share = maxCost > 0 ? (usage.cost / maxCost) * 100 : 0;
  const tokenCount: Record<CostPartKey, number> = {
    input: usage.input,
    output: usage.output,
    cacheRead: usage.cacheRead,
    cacheWrite: usage.cacheWrite,
  };

  // A real button, not a hover-only tooltip: the split is the detail the old
  // table's four token columns carried, so keyboard and touch users need a way
  // to it too. Pointer users still get it on hover.
  return (
    <Popover>
      <PopoverTrigger
        openOnHover
        delay={150}
        render={
          <button
            type="button"
            aria-label={t(($) => $.runs_timeline.cost_breakdown_aria, { cost: formatUsd(usage.cost) })}
          />
        }
        className="flex items-center justify-end gap-2 rounded-xs outline-none focus-visible:ring-2 focus-visible:ring-ring/50"
      >
        <span className="flex w-20 justify-start">
          <CostBar breakdown={breakdown} total={usage.cost} widthPx={(share / 100) * COST_TRACK_PX} />
        </span>
        <span className="w-14 text-right font-medium tabular-nums">{formatUsd(usage.cost)}</span>
      </PopoverTrigger>
      <PopoverContent side="top" align="end" className="w-72 gap-1 text-caption">
        {COST_PARTS.map((part) => (
          <span key={part.key} className="flex items-center gap-2 tabular-nums">
            <span aria-hidden className={cn("size-2 shrink-0 rounded-xs", part.swatch)} />
            <span className="flex-1 text-muted-foreground">{partLabel(part.key)}</span>
            <span className="w-14 text-right text-muted-foreground">
              {formatTokens(tokenCount[part.key])}
            </span>
            <span className="w-14 text-right font-medium">{formatUsd(breakdown[part.key])}</span>
          </span>
        ))}
        {usage.models.length > 0 && (
          <span className="mt-1 border-t pt-1 text-micro text-muted-foreground">
            {usage.models.join(", ")}
          </span>
        )}
      </PopoverContent>
    </Popover>
  );
}

// Width of the cost bar's track (`w-20`).
const COST_TRACK_PX = 80;

// A run's cost as one bar: length is its share of the most expensive run, the
// segments split it by what was billed. Segments too thin to see are dropped
// rather than drawn as a sliver next to a gap, and a bar too short to hold
// its gaps shows only its largest part — gaps alone would swallow it.
function CostBar({
  breakdown,
  total,
  widthPx,
}: {
  breakdown: CostBreakdown;
  total: number;
  widthPx: number;
}) {
  const visible = total > 0
    ? COST_PARTS.filter((part) => breakdown[part.key] / total >= 0.01)
    : [];
  const parts =
    widthPx < 12
      ? visible.toSorted((a, b) => breakdown[b.key] - breakdown[a.key]).slice(0, 1)
      : visible;
  return (
    <span
      aria-hidden
      className="flex h-1 gap-0.5 overflow-hidden rounded-full"
      style={{ width: Math.max(3, widthPx) }}
    >
      {parts.map((part) => (
        <span
          key={part.key}
          className={cn("h-full", part.swatch)}
          style={{ flexGrow: breakdown[part.key], flexBasis: 0 }}
        />
      ))}
    </span>
  );
}
