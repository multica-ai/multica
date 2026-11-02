"use client";

import { useMemo, useRef, useState } from "react";
import { cn } from "@multica/ui/lib/utils";
import { useActorName } from "@multica/core/workspace/hooks";
import { ActorAvatar } from "../../common/actor-avatar";
import { formatDuration } from "../../dashboard/utils";
import { useLocale, useT } from "../../i18n";
import { formatUsd } from "../../runtimes/utils";
import {
  cumulativeCostAt,
  idleSpanAround,
  niceTicks,
  runIndexAt,
  stepCurvePath,
  timeTicks,
  type RunTimeline,
  type TimelineRun,
} from "./issue-run-timeline";
import { RunTriggerLabel } from "./run-trigger-label";
import { useStatusLabel, useTriggerText } from "./task-run-labels";

// The Runs dialog's "over time" view: the issue's cumulative cost as a step
// curve over per-agent run lanes, on one time axis. Where the per-run bars
// answer "which run was expensive", this answers "when did the money go" —
// the bursts, the quiet stretches between them, and the run that moved the
// total most.

// `formatDuration` falls back to this label only below one second.
const UNDER_A_SECOND = "0s";

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

function laneTone(run: TimelineRun, isPeak: boolean): string {
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

export function RunCumulativeChart({
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
    <div className="mt-5 border-b px-6 pb-3">
      <div className="mb-3 flex flex-wrap items-center gap-x-3 gap-y-2">
        {viewSwitch}
        <div className="ml-auto flex items-center gap-4 text-caption text-muted-foreground">
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
      <div
        role="img"
        aria-label={t(($) => $.runs_timeline.cumulative_aria, {
          count: timeline.runs.length,
          cost: formatUsd(timeline.totalCost),
        })}
        className="flex gap-3"
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
                        laneTone(run, run === peak),
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
                  <CumulativeRunCard run={hovered} />
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
    </div>
  );
}
// Axis steps are round numbers; "$50.00" would print noise the curve's end
// label, which keeps full precision, does not need.
function formatTick(v: number): string {
  return Number.isInteger(v) ? `$${v}` : formatUsd(v);
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

function CumulativeRunCard({ run }: { run: TimelineRun }) {
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
