import type { AgentTask } from "@multica/core/types";
import {
  estimateCostBreakdown,
  summarizeTaskUsage,
  type CostBreakdown,
  type TaskUsageSummary,
} from "../../runtimes/utils";
import { taskDurationMs } from "./task-run-labels";

// Pure data for the issue's run timeline: the sidebar's spend strip and the
// Runs dialog it opens. Everything the two surfaces draw — bar positions, the
// cumulative cost curve, day groups — is derived here, so the rendering code
// only maps numbers to pixels and the arithmetic has one place to be tested.

const ACTIVE_STATUSES = new Set<AgentTask["status"]>([
  "queued",
  "dispatched",
  "waiting_local_directory",
  "running",
]);
const TERMINAL_STATUSES = new Set<AgentTask["status"]>([
  "completed",
  "failed",
  "cancelled",
]);

// A run has to cost at least this share of the issue before the chart calls it
// out. Below it no single run explains the curve, and labelling the biggest of
// many similar steps would point at noise.
const PEAK_MIN_SHARE = 0.15;

export interface TimelineRun {
  task: AgentTask;
  /** When the run started working; falls back to dispatch, then creation. */
  startMs: number;
  /** When it finished, or `nowMs` for a run that is still active. */
  endMs: number;
  active: boolean;
  /** Finished-run wall time; null for active runs and runs that never started. */
  durationMs: number | null;
  /** Null when the run recorded no usage — "no figure", never "free". */
  usage: TaskUsageSummary | null;
  breakdown: CostBreakdown | null;
  /** The issue's running total once this run ended — what the curve reads there. */
  costSoFar: number;
}

export interface CumulativeStep {
  t: number;
  cost: number;
}

export interface TimelineLane {
  agentId: string;
  runs: TimelineRun[];
}

export interface RunTimeline {
  /** Oldest first. */
  runs: TimelineRun[];
  totalCost: number;
  pricedCount: number;
  /** Sum of finished runs' wall time. */
  agentMs: number;
  /** First start to last end (or now, while a run is active). */
  elapsedMs: number;
  failedCount: number;
  cancelledCount: number;
  activeCount: number;
  domain: [number, number];
  /** First start to last end — the stretch of the axis that holds runs. */
  extent: [number, number];
  /** The running total after each priced run, in completion order. */
  cumulative: CumulativeStep[];
  /** One lane per agent, in order of first appearance. */
  lanes: TimelineLane[];
  /** The run that moved the total most, when it moved it enough to name. */
  peak: TimelineRun | null;
  /** Highest single-run cost, for scaling per-run bars. */
  maxRunCost: number;
}

function parseMs(iso: string | null | undefined): number | null {
  if (!iso) return null;
  const ms = new Date(iso).getTime();
  return Number.isFinite(ms) ? ms : null;
}

function sumBreakdown(task: AgentTask): CostBreakdown | null {
  if (!task.usage || task.usage.length === 0) return null;
  const total: CostBreakdown = { input: 0, output: 0, cacheRead: 0, cacheWrite: 0 };
  for (const slice of task.usage) {
    const b = estimateCostBreakdown(slice);
    total.input += b.input;
    total.output += b.output;
    total.cacheRead += b.cacheRead;
    total.cacheWrite += b.cacheWrite;
  }
  return total;
}

export function toTimelineRun(task: AgentTask, nowMs: number): TimelineRun | null {
  const active = ACTIVE_STATUSES.has(task.status);
  if (!active && !TERMINAL_STATUSES.has(task.status)) return null;
  const startMs =
    parseMs(task.started_at) ?? parseMs(task.dispatched_at) ?? parseMs(task.created_at);
  if (startMs == null) return null;
  const completedMs = parseMs(task.completed_at);
  // A run cancelled before it ever started has no completion time on some
  // backends; pin it to its start so it still lands on the axis as a sliver.
  const endMs = active ? Math.max(nowMs, startMs) : Math.max(completedMs ?? startMs, startMs);
  const usage = summarizeTaskUsage(task.usage);
  return {
    task,
    startMs,
    endMs,
    active,
    durationMs: active ? null : taskDurationMs(task),
    usage,
    breakdown: usage ? sumBreakdown(task) : null,
    costSoFar: 0,
  };
}

export function buildRunTimeline(tasks: readonly AgentTask[], nowMs: number): RunTimeline {
  const runs = tasks
    .map((task) => toTimelineRun(task, nowMs))
    .filter((run): run is TimelineRun => run !== null)
    .toSorted((a, b) => a.startMs - b.startMs || a.endMs - b.endMs);

  let totalCost = 0;
  let pricedCount = 0;
  let agentMs = 0;
  let failedCount = 0;
  let cancelledCount = 0;
  let activeCount = 0;
  let maxRunCost = 0;
  const laneMap = new Map<string, TimelineRun[]>();

  for (const run of runs) {
    if (run.usage) {
      totalCost += run.usage.cost;
      pricedCount += 1;
      maxRunCost = Math.max(maxRunCost, run.usage.cost);
    }
    if (run.durationMs != null) agentMs += run.durationMs;
    if (run.task.status === "failed") failedCount += 1;
    if (run.task.status === "cancelled") cancelledCount += 1;
    if (run.active) activeCount += 1;
    const lane = laneMap.get(run.task.agent_id);
    if (lane) lane.push(run);
    else laneMap.set(run.task.agent_id, [run]);
  }

  // Usage is written when a run finishes, so the curve steps at completion —
  // and completion order is not start order when runs overlap.
  const cumulative: CumulativeStep[] = [];
  let running = 0;
  for (const run of runs.toSorted((a, b) => a.endMs - b.endMs)) {
    if (run.usage) {
      running += run.usage.cost;
      cumulative.push({ t: run.endMs, cost: running });
    }
    run.costSoFar = running;
  }

  const first = runs[0];
  const lastEnd = runs.reduce((m, r) => Math.max(m, r.endMs), first?.endMs ?? nowMs);
  const elapsedMs = first ? Math.max(0, lastEnd - first.startMs) : 0;

  let peak: TimelineRun | null = null;
  for (const run of runs) {
    if (run.usage && (!peak || run.usage.cost > peak.usage!.cost)) peak = run;
  }
  if (peak && (totalCost <= 0 || peak.usage!.cost / totalCost < PEAK_MIN_SHARE)) peak = null;

  return {
    runs,
    totalCost,
    pricedCount,
    agentMs,
    elapsedMs,
    failedCount,
    cancelledCount,
    activeCount,
    domain: paddedDomain(first?.startMs ?? nowMs, lastEnd),
    extent: [first?.startMs ?? nowMs, lastEnd],
    cumulative,
    lanes: Array.from(laneMap, ([agentId, laneRuns]) => ({ agentId, runs: laneRuns })),
    peak,
    maxRunCost,
  };
}

// A little air on both ends so the first and last bars never sit on the frame,
// and a floor on the span so a single short run doesn't fill the whole axis.
const MIN_SPAN_MS = 60 * 60 * 1000;

function paddedDomain(start: number, end: number): [number, number] {
  let span = end - start;
  if (span < MIN_SPAN_MS) {
    const extra = (MIN_SPAN_MS - span) / 2;
    start -= extra;
    end += extra;
    span = MIN_SPAN_MS;
  }
  const pad = span * 0.02;
  return [start - pad, end + pad];
}

/**
 * Clean y-axis steps (1 / 2 / 2.5 / 5 × 10ⁿ), at most `maxCount` of them, the
 * last one at or above `max` — it doubles as the top of the scale.
 */
export function niceTicks(max: number, maxCount = 4): number[] {
  if (!(max > 0)) return [];
  const magnitude = 10 ** Math.floor(Math.log10(max / maxCount));
  // 10 × magnitude always satisfies the bound, so the fallback never fires.
  const step =
    [1, 2, 2.5, 5, 10].map((m) => m * magnitude).find((s) => max / s <= maxCount) ??
    10 * magnitude;
  const ticks: number[] = [];
  for (let i = 1; ; i++) {
    const v = Number((step * i).toPrecision(12));
    ticks.push(v);
    if (v >= max) break;
  }
  return ticks;
}

/**
 * The run a pointer at time `t` is on: the one whose bar spans `t`, give or
 * take `slopMs` — the time a few pixels cover, so a bar one pixel wide is
 * still something a pointer can land on. -1 between runs: the chart says so,
 * rather than reaching across a gap for a run the pointer is nowhere near.
 * Snapping to the nearest run was what made the crosshair jump — hundreds of
 * pixels for a few pixels of pointer travel.
 *
 * Runs overlap — one agent's short run inside another's long one, or two
 * agents at once — so "the bar it is on" can be several. A bar the pointer is
 * inside beats one it is merely near, then the shortest wins: a bar fully
 * inside another is otherwise unreachable, while the long one still owns
 * every moment the short one doesn't cover. `agentId` narrows the choice to
 * one lane when the pointer is over that lane.
 */
export function runIndexAt(
  runs: readonly TimelineRun[],
  t: number,
  slopMs: number,
  agentId?: string,
): number {
  let best = -1;
  let bestDistance = Infinity;
  let bestSpan = Infinity;
  runs.forEach((run, i) => {
    if (agentId && run.task.agent_id !== agentId) return;
    const distance = t < run.startMs ? run.startMs - t : t > run.endMs ? t - run.endMs : 0;
    if (distance > slopMs) return;
    const span = run.endMs - run.startMs;
    if (distance < bestDistance || (distance === bestDistance && span < bestSpan)) {
      best = i;
      bestDistance = distance;
      bestSpan = span;
    }
  });
  return best;
}

/** What the cumulative curve reads at `t`: the total once every run that had
 *  finished by then was paid for. */
export function cumulativeCostAt(steps: readonly CumulativeStep[], t: number): number {
  let cost = 0;
  for (const step of steps) {
    if (step.t > t) break;
    cost = step.cost;
  }
  return cost;
}

/**
 * The quiet stretch around `t`: when the last run before it ended and the
 * next one after it started. Either side is null past the first or last run.
 * `agentId` reads one lane's quiet stretch.
 */
export function idleSpanAround(
  runs: readonly TimelineRun[],
  t: number,
  agentId?: string,
): { fromMs: number | null; toMs: number | null } {
  let fromMs: number | null = null;
  let toMs: number | null = null;
  for (const run of runs) {
    if (agentId && run.task.agent_id !== agentId) continue;
    if (run.endMs <= t && (fromMs == null || run.endMs > fromMs)) fromMs = run.endMs;
    if (run.startMs >= t && (toMs == null || run.startMs < toMs)) toMs = run.startMs;
  }
  return { fromMs, toMs };
}

/**
 * The cumulative cost as a step curve in a 1000×100 box, for an SVG stretched
 * over the plot with `preserveAspectRatio="none"`. Usage is written when a run
 * finishes, so the line rises at each run's end and holds flat between.
 */
export function stepCurvePath(
  steps: readonly CumulativeStep[],
  [d0, d1]: [number, number],
  yMax: number,
): { line: string; area: string } {
  const x = (t: number) => (((t - d0) / (d1 - d0)) * 1000).toFixed(2);
  const y = (cost: number) => ((1 - cost / yMax) * 100).toFixed(2);
  let line = "M0,100";
  let prevY = "100.00";
  for (const step of steps) {
    const sx = x(step.t);
    const sy = y(step.cost);
    line += ` L${sx},${prevY} L${sx},${sy}`;
    prevY = sy;
  }
  line += ` L1000,${prevY}`;
  return { line, area: `${line} L1000,100 L0,100 Z` };
}

export interface TimeTick {
  t: number;
  /** "day" ticks sit on local midnight; "hour" ticks on a whole hour. */
  kind: "day" | "hour";
}

const HOUR_MS = 60 * 60 * 1000;
const HOUR_STEPS = [1, 2, 3, 6, 12];

/**
 * X-axis ticks in local time. Past a day and a half the axis marks midnights
 * (labelled by weekday); shorter spans mark whole hours at a step that keeps
 * the count readable, because a single day label would leave the axis blank.
 */
export function timeTicks([start, end]: [number, number], maxTicks = 8): TimeTick[] {
  const span = end - start;
  if (!(span > 0)) return [];
  if (span > 36 * HOUR_MS) {
    const ticks: TimeTick[] = [];
    const d = new Date(start);
    d.setHours(0, 0, 0, 0);
    d.setDate(d.getDate() + 1);
    // Thin daily ticks for long issues so the labels never collide.
    const days = Math.ceil(span / (24 * HOUR_MS));
    const every = Math.max(1, Math.ceil(days / maxTicks));
    for (let i = 0; d.getTime() <= end; i++) {
      if (i % every === 0) ticks.push({ t: d.getTime(), kind: "day" });
      d.setDate(d.getDate() + 1);
    }
    return ticks;
  }
  const step = HOUR_STEPS.find((h) => span / (h * HOUR_MS) <= maxTicks) ?? 24;
  const d = new Date(start);
  d.setMinutes(0, 0, 0);
  while (d.getHours() % step !== 0 || d.getTime() <= start) d.setHours(d.getHours() + 1);
  const ticks: TimeTick[] = [];
  for (; d.getTime() < end; d.setHours(d.getHours() + step)) {
    ticks.push({ t: d.getTime(), kind: "hour" });
  }
  return ticks;
}

export interface RunDayGroup {
  /** Local midnight of the day. */
  dayMs: number;
  /** Newest first within the day. */
  runs: TimelineRun[];
  cost: number;
  agentMs: number;
}

/** Newest day first, newest run first — the order people scan a log in. */
export function groupRunsByDay(runs: readonly TimelineRun[]): RunDayGroup[] {
  const groups = new Map<number, RunDayGroup>();
  for (const run of runs.toSorted((a, b) => b.startMs - a.startMs)) {
    const day = new Date(run.startMs);
    day.setHours(0, 0, 0, 0);
    const key = day.getTime();
    let group = groups.get(key);
    if (!group) {
      group = { dayMs: key, runs: [], cost: 0, agentMs: 0 };
      groups.set(key, group);
    }
    group.runs.push(run);
    group.cost += run.usage?.cost ?? 0;
    group.agentMs += run.durationMs ?? 0;
  }
  return Array.from(groups.values());
}
