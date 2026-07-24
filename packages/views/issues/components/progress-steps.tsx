import { useLayoutEffect, useRef, useState } from "react";
import { Check, X, Loader2, Bot } from "lucide-react";
import {
  Tooltip,
  TooltipTrigger,
  TooltipContent,
} from "@multica/ui/components/ui/tooltip";
import { useT } from "../../i18n";

export interface ProgressPhase {
  /** Stable node id, required only in DAG mode (when `edges` is present). */
  id?: string;
  name: string;
  status: "completed" | "running" | "failed" | "pending";
  started_at?: string;
  finished_at?: string;
  detail?: string;
  agent?: string;
  /** Delivery comment for this phase. Carried in the schema for consumers that
   * want to link a phase to its report; the renderer itself does not act on it. */
  comment_id?: string;
  summary?: string;
}

/** A directed connection between two phases, referencing `phase.id`. */
export interface ProgressEdge {
  from: string;
  to: string;
}

export interface ProgressData {
  phases: ProgressPhase[];
  /** When present and non-empty (and valid), the progress renders as a 2D DAG. */
  edges?: ProgressEdge[];
  current_phase?: number;
  updated_at?: string;
}

const STATUS_CONFIG = {
  completed: {
    icon: Check,
    dotClass: "bg-green-500 text-white shadow-sm shadow-green-500/30",
    labelClass: "text-foreground",
  },
  running: {
    icon: Loader2,
    dotClass: "bg-blue-500 text-white shadow-md shadow-blue-500/40 ring-4 ring-blue-500/20",
    labelClass: "text-foreground font-medium",
    iconClass: "animate-spin",
  },
  failed: {
    icon: X,
    dotClass: "bg-red-500 text-white shadow-sm shadow-red-500/30",
    labelClass: "text-red-500",
  },
  pending: {
    icon: undefined,
    dotClass: "bg-muted border-2 border-dashed border-muted-foreground/30 text-muted-foreground/50",
    labelClass: "text-muted-foreground",
  },
} as const;

function lineClass(from: ProgressPhase, to: ProgressPhase): string {
  const failed = from.status === "failed" || to.status === "failed";
  const advanced = from.status === "completed" || from.status === "running";
  const targetPending = to.status === "pending";

  if (failed) return "bg-red-500";
  if (advanced && !targetPending) return "bg-green-500";
  if (advanced && targetPending) return "bg-gradient-to-r from-green-500 to-muted-foreground/20";
  return "bg-muted-foreground/20";
}

/**
 * Resolve a DAG layout from phases + edges into ordered columns.
 *
 * Returns `null` when the data cannot form a valid DAG — missing ids, edges
 * pointing at unknown ids, or a cycle. Callers fall back to linear rendering,
 * matching the codebase rule that data drift downgrades rather than crashes.
 */
export interface DagColumn {
  phases: ProgressPhase[];
}

export function computeDagLayout(
  phases: ProgressPhase[],
  edges: ProgressEdge[],
): DagColumn[] | null {
  // Every phase must carry a unique id to be addressable by edges.
  const byId = new Map<string, ProgressPhase>();
  for (const p of phases) {
    if (!p.id || byId.has(p.id)) return null;
    byId.set(p.id, p);
  }

  // Build adjacency + in-degree; reject edges referencing unknown ids.
  const adjacency = new Map<string, string[]>();
  const indegree = new Map<string, number>();
  for (const id of byId.keys()) {
    adjacency.set(id, []);
    indegree.set(id, 0);
  }
  for (const e of edges) {
    if (!byId.has(e.from) || !byId.has(e.to)) return null;
    adjacency.get(e.from)!.push(e.to);
    indegree.set(e.to, indegree.get(e.to)! + 1);
  }

  // Kahn's algorithm: longest-path layering so a node sits one column past
  // all of its predecessors (joins land after every branch feeding them).
  const level = new Map<string, number>();
  const queue: string[] = [];
  for (const [id, deg] of indegree) {
    if (deg === 0) {
      level.set(id, 0);
      queue.push(id);
    }
  }
  let processed = 0;
  while (queue.length > 0) {
    const id = queue.shift()!;
    processed += 1;
    const col = level.get(id)!;
    for (const next of adjacency.get(id)!) {
      level.set(next, Math.max(level.get(next) ?? 0, col + 1));
      const deg = indegree.get(next)! - 1;
      indegree.set(next, deg);
      if (deg === 0) queue.push(next);
    }
  }
  // Not every node consumed → there is a cycle.
  if (processed !== phases.length) return null;

  const maxLevel = Math.max(...Array.from(level.values()), 0);
  const columns: DagColumn[] = Array.from({ length: maxLevel + 1 }, () => ({
    phases: [],
  }));
  // Preserve declaration order within a column for stable vertical stacking.
  for (const p of phases) {
    columns[level.get(p.id!)!]!.phases.push(p);
  }
  return columns;
}

function formatTimeShort(iso?: string): string | undefined {
  if (!iso) return undefined;
  try {
    const d = new Date(iso);
    const m = String(d.getMonth() + 1).padStart(2, "0");
    const day = String(d.getDate()).padStart(2, "0");
    const hh = String(d.getHours()).padStart(2, "0");
    const mm = String(d.getMinutes()).padStart(2, "0");
    return `${m}-${day} ${hh}:${mm}`;
  } catch {
    return iso;
  }
}

function durationLabel(start?: string, end?: string): string | undefined {
  if (!start || !end) return undefined;
  try {
    const ms = new Date(end).getTime() - new Date(start).getTime();
    if (!Number.isFinite(ms) || ms < 0) return undefined;
    const sec = Math.round(ms / 1000);
    if (sec < 60) return `${sec}s`;
    const min = Math.round(sec / 60);
    if (min < 60) return `${min}min`;
    const hr = Math.floor(min / 60);
    const rem = min % 60;
    return rem ? `${hr}h ${rem}min` : `${hr}h`;
  } catch {
    return undefined;
  }
}

export function ProgressSteps({
  phases,
  edges,
}: {
  phases: ProgressPhase[];
  edges?: ProgressEdge[];
}) {
  if (phases.length === 0) return null;

  // DAG mode only when edges are declared AND form a valid layout; otherwise
  // fall back to the linear rendering (handles history data + cycles).
  const dagColumns = edges?.length ? computeDagLayout(phases, edges) : null;
  if (dagColumns) {
    return <DagLayout columns={dagColumns} edges={edges!} />;
  }

  return (
    <div className="w-full px-1 py-2">
      <div className="flex w-full items-start">
        {phases.map((phase, i) => {
          const isLast = i === phases.length - 1;
          return (
            <div key={i} className="flex min-w-0 flex-1 items-start last:flex-none">
              <PhaseNode phase={phase} index={i} />
              {/* Connecting line */}
              {!isLast && phases[i + 1] && (
                <div
                  className={`mt-5 h-0.5 flex-1 rounded-full ${lineClass(phase, phases[i + 1]!)}`}
                />
              )}
            </div>
          );
        })}
      </div>
    </div>
  );
}

/**
 * A single progress node (dot + label + tooltip). Shared by the linear and DAG
 * layouts so node appearance/behavior stays identical across both.
 */
function PhaseNode({
  phase,
  index,
  nodeRef,
}: {
  phase: ProgressPhase;
  index: number;
  nodeRef?: (el: HTMLButtonElement | null) => void;
}) {
  const { t } = useT("issues");
  const config = STATUS_CONFIG[phase.status] ?? STATUS_CONFIG.pending;
  const Icon = config.icon;
  const duration = durationLabel(phase.started_at, phase.finished_at);

  return (
    <div className="flex min-w-0 flex-col items-center px-1">
      <Tooltip>
        <TooltipTrigger
          render={
            // Inert button — it carries the tooltip trigger but has no action.
            <button
              ref={nodeRef}
              type="button"
              disabled
              className={`flex size-10 shrink-0 cursor-default items-center justify-center rounded-full transition-all ${config.dotClass}`}
            >
              {Icon ? (
                <Icon
                  className={`size-5 ${"iconClass" in config ? config.iconClass : ""}`}
                  strokeWidth={3}
                />
              ) : (
                <span className="text-sm font-semibold">{index + 1}</span>
              )}
            </button>
          }
        />
        <TooltipContent side="bottom" sideOffset={8} className="!block w-72 max-w-72 p-0">
          <PhaseTooltip
            phase={phase}
            duration={duration}
            labels={{
              started: t(($) => $.detail.progress_started),
              finished: t(($) => $.detail.progress_finished),
              running: t(($) => $.detail.progress_running),
              failed: t(($) => $.detail.progress_failed),
            }}
          />
        </TooltipContent>
      </Tooltip>

      <div className="mt-2 flex w-full max-w-[120px] flex-col items-center gap-0.5">
        <span
          className={`w-full truncate text-center text-sm font-medium leading-tight ${config.labelClass}`}
          title={phase.name}
        >
          {phase.name}
        </span>
        {phase.agent && (
          <span className="w-full truncate text-center text-xs text-muted-foreground/70">
            {phase.agent}
          </span>
        )}
      </div>
    </div>
  );
}

interface EdgePath {
  d: string;
  className: string;
}

/**
 * 2D DAG layout: columns laid out left-to-right, nodes stacked vertically
 * within a column, edges drawn as cubic Béziers on an SVG overlay. Node anchor
 * points are measured from the DOM after layout (and on resize) so the curves
 * track the real node positions regardless of column heights.
 */
/**
 * Smallest scale we allow before giving up and letting the graph scroll.
 * Below this the labels (text-sm → ~9.8px, the agent text-xs → ~8.4px) stop
 * being comfortably readable, so a wider graph scrolls rather than shrinking
 * the text into illegibility.
 */
const MIN_DAG_SCALE = 0.7;

/** Vertical room reserved for the horizontal scrollbar so it doesn't clip nodes. */
const SCROLLBAR_ALLOWANCE = 16;

function DagLayout({
  columns,
  edges,
}: {
  columns: DagColumn[];
  edges: ProgressEdge[];
}) {
  const containerRef = useRef<HTMLDivElement | null>(null);
  const scaledRef = useRef<HTMLDivElement | null>(null);
  const columnsRef = useRef<HTMLDivElement | null>(null);
  const nodeEls = useRef(new Map<string, HTMLButtonElement>());
  const [paths, setPaths] = useState<EdgePath[]>([]);
  // Fit-to-width: scale the whole graph down (never up) so it fits the issue
  // body, but never below MIN_DAG_SCALE — past that we scroll instead. The
  // outer height is reserved as naturalHeight * scale so shrinking the graph
  // doesn't leave a gap underneath it.
  const [scale, setScale] = useState(1);
  const [scaledHeight, setScaledHeight] = useState<number | null>(null);
  // Mirror the applied scale into a ref so the measure callback can read the
  // current value without listing `scale` as an effect dependency (which would
  // re-subscribe the ResizeObserver on every scale change).
  const scaleRef = useRef(scale);
  scaleRef.current = scale;

  // Flat id → phase map for edge color lookup.
  const phaseById = new Map<string, ProgressPhase>();
  for (const col of columns) {
    for (const p of col.phases) phaseById.set(p.id!, p);
  }

  // Changes when the graph's shape or any node status changes — the only times
  // anchors/colors need re-measuring. Drives the effect's dependency array.
  const graphSignature =
    columns.map((c) => c.phases.map((p) => `${p.id}:${p.status}`).join(",")).join("|") +
    "#" +
    edges.map((e) => `${e.from}>${e.to}`).join(",");

  useLayoutEffect(() => {
    const container = containerRef.current;
    const scaled = scaledRef.current;
    if (!container || !scaled) return;

    const recompute = () => {
      // Natural (pre-transform) size — layout dimensions are unaffected by the
      // CSS transform, so these stay stable even while a scale is applied.
      const cols = columnsRef.current;
      if (!cols) return;
      // Measure the columns grid directly (not the scaled wrapper, whose
      // absolutely-positioned SVG child can skew scrollHeight). offsetWidth/Height
      // are pre-transform layout dims, so they stay stable across scale changes.
      const naturalWidth = cols.offsetWidth;
      const naturalHeight = cols.offsetHeight;
      const available = container.clientWidth;
      const nextScale =
        naturalWidth > available && naturalWidth > 0
          ? Math.max(MIN_DAG_SCALE, available / naturalWidth)
          : 1;
      // Reserve the scaled content height plus room for the horizontal scrollbar
      // when the floored scale still overflows the available width. The small
      // unconditional buffer absorbs sub-pixel rounding so a stray vertical
      // scrollbar never appears.
      const scrolls = naturalWidth * nextScale > available + 1;
      const nextHeight =
        naturalHeight * nextScale + 2 + (scrolls ? SCROLLBAR_ALLOWANCE : 0);
      // Guard every setState so a converged layout stops re-rendering. The
      // effect runs on every render (it must, to re-measure after the scale
      // transform applies), so unconditional setState here would loop forever
      // — setPaths in particular always builds a fresh array reference.
      setScale((prev) => (Math.abs(prev - nextScale) < 0.001 ? prev : nextScale));
      setScaledHeight((prev) =>
        prev != null && Math.abs(prev - nextHeight) < 0.5 ? prev : nextHeight,
      );

      // Anchor points are read from the live (already-scaled) rects, and the
      // SVG lives inside the same scaled layer, so the relative coordinates are
      // self-consistent — the curves track the nodes at any scale.
      const base = scaled.getBoundingClientRect();
      const next: EdgePath[] = [];
      for (const e of edges) {
        const fromEl = nodeEls.current.get(e.from);
        const toEl = nodeEls.current.get(e.to);
        const fromPhase = phaseById.get(e.from);
        const toPhase = phaseById.get(e.to);
        if (!fromEl || !toEl || !fromPhase || !toPhase) continue;
        const a = fromEl.getBoundingClientRect();
        const b = toEl.getBoundingClientRect();
        // Convert viewport rects back to the scaled layer's own coordinate
        // space (divide out the scale) so the SVG path stays in natural px.
        // The rects already carry the *currently applied* scale, read from the
        // ref (not `nextScale`, which only takes effect next frame).
        const s = scaleRef.current || 1;
        const x1 = (a.right - base.left) / s;
        const y1 = (a.top + a.height / 2 - base.top) / s;
        const x2 = (b.left - base.left) / s;
        const y2 = (b.top + b.height / 2 - base.top) / s;
        const dx = Math.max(24, (x2 - x1) / 2);
        const d = `M ${x1} ${y1} C ${x1 + dx} ${y1}, ${x2 - dx} ${y2}, ${x2} ${y2}`;
        next.push({ d, className: strokeClass(lineClass(fromPhase, toPhase)) });
      }
      // Only update paths when they actually change, otherwise the new array
      // reference would re-trigger this effect indefinitely.
      setPaths((prev) => (samePaths(prev, next) ? prev : next));
    };

    recompute();
    const ro = new ResizeObserver(recompute);
    ro.observe(container);
    ro.observe(scaled);
    return () => ro.disconnect();
    // Re-subscribe only when the graph itself changes (node statuses/positions
    // or edges). `graphSignature` captures that; `scale` is read via ref so it
    // is intentionally not a dependency.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [graphSignature]);

  return (
    <div
      ref={containerRef}
      className="relative w-full overflow-x-auto overflow-y-hidden px-1 py-2"
      style={scaledHeight != null ? { height: scaledHeight } : undefined}
    >
      <div
        ref={scaledRef}
        className="relative inline-block origin-top-left"
        style={{ transform: `scale(${scale})` }}
      >
        {/* Edge overlay sits behind nodes; nodes capture pointer events. */}
        <svg className="pointer-events-none absolute inset-0 size-full" aria-hidden="true">
          {paths.map((p, i) => (
            <path
              key={i}
              d={p.d}
              fill="none"
              strokeWidth={2}
              className={p.className}
              strokeLinecap="round"
            />
          ))}
        </svg>
        <div ref={columnsRef} className="relative flex items-stretch gap-x-10">
          {columns.map((col, ci) => (
            <div key={ci} className="flex flex-col justify-center gap-y-6">
              {col.phases.map((phase, pi) => (
                <PhaseNode
                  key={phase.id ?? pi}
                  phase={phase}
                  index={pi}
                  nodeRef={(el) => {
                    if (el) nodeEls.current.set(phase.id!, el);
                    else nodeEls.current.delete(phase.id!);
                  }}
                />
              ))}
            </div>
          ))}
        </div>
      </div>
    </div>
  );
}

/** Structural equality for edge paths so a converged layout stops re-rendering. */
function samePaths(a: EdgePath[], b: EdgePath[]): boolean {
  if (a.length !== b.length) return false;
  for (let i = 0; i < a.length; i++) {
    if (a[i]!.d !== b[i]!.d || a[i]!.className !== b[i]!.className) return false;
  }
  return true;
}

/** Map the linear `lineClass` (Tailwind bg-*) to an SVG stroke equivalent. */
function strokeClass(line: string): string {
  if (line.includes("red")) return "stroke-red-500";
  if (line.includes("green")) return "stroke-green-500";
  return "stroke-muted-foreground/30";
}

function PhaseTooltip({
  phase,
  duration,
  labels,
}: {
  phase: ProgressPhase;
  duration: string | undefined;
  labels: {
    started: string;
    finished: string;
    running: string;
    failed: string;
  };
}) {
  const startedLabel = formatTimeShort(phase.started_at);
  const finishedLabel = formatTimeShort(phase.finished_at);

  return (
    <div className="text-left">
      {/* Header */}
      <div className="border-b border-border px-3 py-2">
        <div className="text-sm font-semibold leading-tight text-foreground">
          {phase.name}
        </div>
      </div>

      {/* Body — agent + summary */}
      {(phase.agent || phase.summary) && (
        <div className="space-y-1.5 border-b border-border px-3 py-2">
          {phase.agent && (
            <div className="flex items-center gap-1.5 text-xs text-muted-foreground">
              <Bot className="size-3.5 shrink-0" />
              <span className="truncate font-medium text-foreground">{phase.agent}</span>
            </div>
          )}
          {phase.summary && (
            <div className="text-xs leading-relaxed text-muted-foreground whitespace-normal break-words">
              {phase.summary}
            </div>
          )}
        </div>
      )}

      {/* Times */}
      {(startedLabel || finishedLabel || phase.status === "running") && (
        <div className="space-y-1 px-3 py-2 text-xs">
          {startedLabel && (
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">{labels.started}</span>
              <span className="tabular-nums text-foreground">{startedLabel}</span>
            </div>
          )}
          {finishedLabel && (
            <div className="flex items-center justify-between gap-3">
              <span className="text-muted-foreground">{labels.finished}</span>
              <span className="tabular-nums text-foreground">
                {finishedLabel}
                {duration && (
                  <span className="ml-1.5 text-muted-foreground/70">({duration})</span>
                )}
              </span>
            </div>
          )}
          {phase.status === "running" && !finishedLabel && (
            <div className="flex items-center gap-1.5 text-blue-500">
              <Loader2 className="size-3 animate-spin" />
              <span>{labels.running}</span>
            </div>
          )}
        </div>
      )}

      {/* Failure detail */}
      {phase.status === "failed" && phase.detail && (
        <div className="border-t border-border bg-red-500/5 px-3 py-2 text-xs text-red-500">
          <div className="font-medium">{labels.failed}</div>
          <div className="mt-0.5 whitespace-normal break-words">{phase.detail}</div>
        </div>
      )}
    </div>
  );
}
