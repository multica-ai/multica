import type { TimelineItem } from "./build-timeline";

/**
 * A run's steps, derived from the flat event stream.
 *
 * The stream stores a tool call and its result as two independent rows
 * (`tool_use` then `tool_result`), which is how the transcript ended up twice
 * as long as the work it describes: a 75-call run rendered 150 rows, and the
 * output row was where raw base64 and 8KB command dumps landed. A step folds
 * the pair back together, so one call reads as one line and its result is that
 * line's detail.
 *
 * Pairing is positional because the events carry no call id. `agent.Message`
 * has `CallID` all the way to the daemon, but `TaskMessageData` drops it before
 * the report and `task_message` has no column for it — until that lands, a
 * result belongs to the oldest still-open call with the same tool name.
 */

/** One tool call. Either side can be missing: a call still running has no
 *  result yet, and a stream that reconnected mid-flight can deliver a result
 *  whose call was never recorded. */
export interface TraceCallStep {
  kind: "call";
  /** Ordering key: the call's seq, or the result's when the call is missing. */
  seq: number;
  tool: string;
  call?: TimelineItem;
  result?: TimelineItem;
  startedAt?: string;
  endedAt?: string;
  /** Wall-clock ms between call and result. Undefined when either side is
   *  missing a timestamp — never guessed. */
  durationMs?: number;
}

/** Agent prose, model thinking, or an error: one message, nothing to pair. */
export interface TraceMessageStep {
  kind: "text" | "thinking" | "error";
  seq: number;
  item: TimelineItem;
  startedAt?: string;
}

export type TraceStep = TraceCallStep | TraceMessageStep;

/** Consecutive same-tool calls, folded. Expands back to its members. */
export interface TraceGroupRow {
  kind: "group";
  seq: number;
  tool: string;
  steps: TraceCallStep[];
  startedAt?: string;
  endedAt?: string;
  durationMs?: number;
}

export type TraceRow = TraceStep | TraceGroupRow;

/**
 * How many consecutive same-tool calls it takes to fold into one row.
 *
 * Three, not two: two file reads are still two lines worth reading, five are
 * one act.
 */
export const MIN_GROUP_SIZE = 3;

/**
 * A shell call never folds, however many run in a row.
 *
 * Reading a run, `pnpm typecheck` and `pnpm exec playwright test` are two
 * different things that happened; "Bash · 5 calls" hides exactly what the
 * reader came for, and a group duration spanning the gaps between them reads
 * as one ten-minute command. Keyed on the input carrying a `command` string
 * rather than a tool-name allowlist, so it holds for every backend.
 */
function isShellCall(step: TraceCallStep): boolean {
  return typeof step.call?.input?.command === "string";
}

function timeMs(iso: string | undefined): number | undefined {
  if (!iso) return undefined;
  const ms = new Date(iso).getTime();
  return Number.isFinite(ms) ? ms : undefined;
}

function durationBetween(start?: string, end?: string): number | undefined {
  const a = timeMs(start);
  const b = timeMs(end);
  if (a === undefined || b === undefined) return undefined;
  // Clamp rather than drop: the daemon flushes in 500ms batches, so a fast
  // call can land with its result on the same tick or one tick earlier.
  return Math.max(0, b - a);
}

/** Fold `tool_use` / `tool_result` pairs into single steps, in stream order. */
export function buildSteps(items: TimelineItem[]): TraceStep[] {
  const steps: TraceStep[] = [];
  // Open calls per tool, oldest first. FIFO rather than nearest-preceding:
  // when a provider runs two calls of the same tool in parallel it returns
  // them in call order more often than in reverse.
  const open = new Map<string, TraceCallStep[]>();

  for (const item of items) {
    if (item.type === "tool_use") {
      const tool = item.tool ?? "";
      const step: TraceCallStep = {
        kind: "call",
        seq: item.seq,
        tool,
        call: item,
        startedAt: item.created_at,
      };
      steps.push(step);
      const queue = open.get(tool);
      if (queue) queue.push(step);
      else open.set(tool, [step]);
      continue;
    }

    if (item.type === "tool_result") {
      const tool = item.tool ?? "";
      const pending = open.get(tool)?.shift();
      if (pending) {
        pending.result = item;
        pending.endedAt = item.created_at;
        pending.durationMs = durationBetween(pending.startedAt, item.created_at);
        continue;
      }
      // Orphan result: keep it as a step of its own so no output is dropped.
      steps.push({
        kind: "call",
        seq: item.seq,
        tool,
        result: item,
        startedAt: item.created_at,
        endedAt: item.created_at,
      });
      continue;
    }

    steps.push({
      kind: item.type,
      seq: item.seq,
      item,
      startedAt: item.created_at,
    });
  }

  return steps;
}

/** Fold runs of `MIN_GROUP_SIZE`+ consecutive same-tool calls into group rows. */
export function groupSteps(steps: TraceStep[]): TraceRow[] {
  const rows: TraceRow[] = [];
  let run: TraceCallStep[] = [];

  const flush = () => {
    if (run.length === 0) return;
    if (run.length < MIN_GROUP_SIZE) {
      rows.push(...run);
    } else {
      const first = run[0]!;
      const last = run[run.length - 1]!;
      rows.push({
        kind: "group",
        seq: first.seq,
        tool: first.tool,
        steps: run,
        startedAt: first.startedAt,
        endedAt: last.endedAt,
        durationMs: durationBetween(first.startedAt, last.endedAt),
      });
    }
    run = [];
  };

  for (const step of steps) {
    if (
      step.kind === "call" &&
      !isShellCall(step) &&
      (run.length === 0 || run[0]!.tool === step.tool)
    ) {
      run.push(step);
      continue;
    }
    flush();
    if (step.kind === "call") run.push(step);
    else rows.push(step);
  }
  flush();

  return rows;
}

// `TraceMessageStep` carries three kinds on one interface, so a `kind` check
// alone narrows the property without dropping the constituent. These predicates
// are what let callers switch on a row and get a usable type back.
export function isGroupRow(row: TraceRow): row is TraceGroupRow {
  return row.kind === "group";
}

export function isCallStep(row: TraceRow): row is TraceCallStep {
  return row.kind === "call";
}

export function isMessageStep(row: TraceRow): row is TraceMessageStep {
  return row.kind === "text" || row.kind === "thinking" || row.kind === "error";
}

/** Every call inside a row, so a group and a lone call read the same way. */
export function rowCalls(row: TraceRow): TraceCallStep[] {
  if (row.kind === "group") return row.steps;
  return row.kind === "call" ? [row] : [];
}

// ─── Lanes ──────────────────────────────────────────────────────────────────

export type LaneSegmentKind = "tool" | "think" | "report" | "error";

export interface LaneSegment {
  /** ms from the run's start. */
  startMs: number;
  durationMs: number;
  kind: LaneSegmentKind;
}

export interface TraceLanes {
  /** Model turns: the complement of the tool lane. */
  model: LaneSegment[];
  tool: LaneSegment[];
  modelMs: number;
  toolMs: number;
  totalMs: number;
}

interface Interval {
  start: number;
  end: number;
}

function mergeIntervals(intervals: Interval[]): Interval[] {
  const sorted = [...intervals].sort((a, b) => a.start - b.start);
  const out: Interval[] = [];
  for (const interval of sorted) {
    const prev = out[out.length - 1];
    if (prev && interval.start <= prev.end) {
      prev.end = Math.max(prev.end, interval.end);
      continue;
    }
    out.push({ ...interval });
  }
  return out;
}

/**
 * Two lanes: what the tools were doing, and what the model was doing.
 *
 * The model lane is derived as the complement of the tool lane rather than
 * from the model's own events, because that is the only honest source we have
 * — a `text` or `thinking` row carries one arrival timestamp, not a span. The
 * gaps between tool calls ARE the model's time, which is what makes the
 * "28 minutes went where" question answerable at all.
 *
 * Returns null when the events carry no usable timestamps; callers drop the
 * timeline entirely rather than draw an axis that means nothing.
 */
export function buildLanes(
  steps: TraceStep[],
  runStart: string | undefined,
  runEnd: string | undefined,
): TraceLanes | null {
  const stamps = steps
    .flatMap((step) => [timeMs(step.startedAt), timeMs(step.kind === "call" ? step.endedAt : undefined)])
    .filter((ms): ms is number => ms !== undefined);
  if (stamps.length === 0) return null;

  const startMs = Math.min(timeMs(runStart) ?? Infinity, ...stamps);
  const endMs = Math.max(timeMs(runEnd) ?? -Infinity, ...stamps);
  const totalMs = endMs - startMs;
  if (!Number.isFinite(totalMs) || totalMs <= 0) return null;

  const calls: Interval[] = [];
  for (const step of steps) {
    if (step.kind !== "call") continue;
    const start = timeMs(step.startedAt);
    const end = timeMs(step.endedAt);
    if (start === undefined || end === undefined || end <= start) continue;
    calls.push({
      start: Math.max(startMs, start),
      end: Math.min(endMs, end),
    });
  }

  const merged = mergeIntervals(calls.filter((c) => c.end > c.start));
  const tool: LaneSegment[] = merged.map((c) => ({
    startMs: c.start - startMs,
    durationMs: c.end - c.start,
    kind: "tool",
  }));

  // Model segments fill every gap the tools left, including the head and tail.
  const model: LaneSegment[] = [];
  let cursor = startMs;
  const pushModel = (from: number, to: number) => {
    if (to <= from) return;
    model.push({ startMs: from - startMs, durationMs: to - from, kind: kindForGap(steps, from, to) });
  };
  for (const interval of merged) {
    pushModel(cursor, interval.start);
    cursor = Math.max(cursor, interval.end);
  }
  pushModel(cursor, endMs);

  return {
    model,
    tool,
    modelMs: model.reduce((sum, s) => sum + s.durationMs, 0),
    toolMs: tool.reduce((sum, s) => sum + s.durationMs, 0),
    totalMs,
  };
}

/**
 * What the model was doing in this gap, judged by the messages that landed in
 * it: an error outranks a report, a report outranks plain thinking. Colour
 * therefore carries state (fine / delivered / failed), never taxonomy — the
 * token set has no categorical ramp to spend on five event types.
 */
function kindForGap(steps: TraceStep[], from: number, to: number): LaneSegmentKind {
  let kind: LaneSegmentKind = "think";
  for (const step of steps) {
    if (step.kind === "call") continue;
    const at = timeMs(step.startedAt);
    if (at === undefined || at < from || at > to) continue;
    if (step.kind === "error") return "error";
    if (step.kind === "text") kind = "report";
  }
  return kind;
}

/**
 * Axis ticks at a round interval.
 *
 * `maxTicks` scales with zoom: a timeline stretched to 4x its container has
 * four screenfuls to label, so it needs four times the ticks to keep the same
 * on-screen density.
 */
export function timelineTicks(totalMs: number, maxTicks = 6): number[] {
  if (!Number.isFinite(totalMs) || totalMs <= 0) return [];
  const steps = [
    1_000, 5_000, 15_000, 30_000, 60_000, 2 * 60_000, 5 * 60_000, 10 * 60_000,
    15 * 60_000, 30 * 60_000, 60 * 60_000, 2 * 60 * 60_000, 6 * 60 * 60_000,
  ];
  const interval = steps.find((step) => totalMs / step <= maxTicks) ?? steps[steps.length - 1]!;
  const ticks: number[] = [];
  for (let at = 0; at < totalMs; at += interval) ticks.push(at);
  ticks.push(totalMs);
  return ticks;
}

/** A bar thinner than this would vanish; a fast call has to stay clickable. */
const SEGMENT_MIN_PX = 3;

/**
 * Where one segment sits on its lane, as CSS.
 *
 * The width floor is what makes this more than two percentages: a 40ms call in
 * a 40-minute run rounds to a fraction of a pixel, so every bar is given a
 * visible minimum and zooming is what makes its width truthful. That floor is
 * also why `left` is clamped — a sliver starting at 99.99% would otherwise
 * paint its 3px past the end of the axis, and those 3px of scrollable overflow
 * hand the track a horizontal scrollbar it has nothing to scroll with (#6278).
 */
export function laneSegmentPosition(
  segment: LaneSegment,
  totalMs: number,
): { left: string; width: string } {
  const width = `max(${SEGMENT_MIN_PX}px, ${(segment.durationMs / totalMs) * 100}%)`;
  return { left: `min(${(segment.startMs / totalMs) * 100}%, 100% - ${width})`, width };
}

/**
 * How the tool lane's time splits by what the calls were doing.
 *
 * The lane itself stays whole: in a real run one kind dominates (commands run
 * for minutes, reads and edits finish in milliseconds), so a lane per kind
 * would draw three near-empty tracks to say what one line of text says. This
 * is that line of text.
 */
export interface ToolKindTotals {
  command: number;
  write: number;
  read: number;
  other: number;
}

export function toolKindTotals(steps: TraceStep[]): ToolKindTotals {
  const totals: ToolKindTotals = { command: 0, write: 0, read: 0, other: 0 };
  for (const step of steps) {
    if (step.kind !== "call" || step.durationMs === undefined) continue;
    const input = step.call?.input;
    const kind: keyof ToolKindTotals =
      typeof input?.command === "string"
        ? "command"
        : typeof input?.old_string === "string" ||
            typeof input?.content === "string" ||
            input?.changes !== undefined
          ? "write"
          : typeof input?.file_path === "string" ||
              typeof input?.path === "string" ||
              typeof input?.pattern === "string" ||
              typeof input?.query === "string"
            ? "read"
            : "other";
    totals[kind] += step.durationMs;
  }
  return totals;
}

/** Below these, a timeline is chrome: it would render a handful of bars that
 *  say less than the durations already on each row. */
export const TIMELINE_MIN_STEPS = 8;
export const TIMELINE_MIN_MS = 60_000;

export function shouldShowTimeline(steps: TraceStep[], lanes: TraceLanes | null): boolean {
  if (!lanes) return false;
  return steps.length >= TIMELINE_MIN_STEPS && lanes.totalMs >= TIMELINE_MIN_MS;
}
