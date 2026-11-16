// What ran during a long task, from the two sources that can say: the Long
// Animation Frames API (Chromium), which names the script entry points of a
// slow frame, and the `performance.measure` entries React's development build
// emits for its Performance tracks, which name the components that rendered
// and what scheduled the update. Both are buffered briefly so the dev recorder
// can attach them to a long task once the browser reports it.

/** A script that ran during the task, as the Long Animation Frames API reports it. */
export interface DevLongTaskScript {
  /** What called into the script, such as `MessagePort.onmessage` or `Response.json.then`. */
  invoker: string;
  /** `user-callback`, `event-listener`, `resolve-promise`, `classic-script`, … */
  invokerType: string;
  functionName: string | null;
  /** `url:charPosition` of the function, when the engine reports it. */
  source: string | null;
  durationMs: number;
  /** Style and layout the script forced by reading layout after changing the DOM. */
  forcedLayoutMs: number;
}

/** The slow frame the task belongs to. */
export interface DevLongTaskFrame {
  durationMs: number;
  blockingMs: number;
  /** From the start of rendering (animation frame callbacks, style, layout) to the frame's end. */
  renderMs: number;
  /** The style and layout part of `renderMs`. */
  styleLayoutMs: number;
}

/** A React update whose render began during the task. */
export interface DevReactUpdate {
  /** `Update`, `Cascading Update`, `Update Blocked` or `Promise Resolved`. */
  kind: string;
  /** React lane track: `Blocking`, `Transition`, … */
  lane: string | null;
  /** Component that scheduled the update. */
  component: string | null;
  /** How it was scheduled, such as `setState`. */
  method: string | null;
}

/** Renders of one component during the task. React only measures renders whose props changed. */
export interface DevComponentRender {
  name: string;
  renders: number;
  /** Render time inside the component, less the measured components it rendered. */
  selfMs: number;
  /** Top-level props that changed, by name. */
  changedProps: string[];
  /** React found the new props deeply equal to the old ones: a new object with the same contents. */
  deeplyEqualProps: boolean;
}

export interface DevLongTaskAttribution {
  /** Null where the engine does not report long animation frames. */
  frame: DevLongTaskFrame | null;
  /** Longest first. */
  scripts: DevLongTaskScript[];
  /** In the order their renders began. */
  updates: DevReactUpdate[];
  /** Renders that began and ended inside the task, most self time first. */
  components: DevComponentRender[];
  /**
   * Time-sliced renders that cross the task's start or end. Their spans include
   * time React yielded to other work, so they are leads with no time charged.
   */
  spanningComponents: { name: string; renders: number }[];
}

/** Performance-timeline span: ms since `performance.timeOrigin`. */
interface Span {
  start: number;
  end: number;
}

interface FrameRecord extends Span {
  blockingMs: number;
  renderStart: number;
  styleAndLayoutStart: number;
  scripts: (Span & Omit<DevLongTaskScript, "durationMs">)[];
}

interface MeasureRecord extends Span {
  name: string;
  track: string | null;
  trackGroup: string | null;
  properties: readonly unknown[] | null;
}

const COMPONENTS_TRACK = "Components \u269b";
const SCHEDULER_TRACK_GROUP = "Scheduler \u269b";
// React prefixes component render measures with a zero-width space, which
// keeps them apart from its other measures on the same track.
const COMPONENT_MEASURE_PREFIX = "\u200b";
const DEEP_EQUALITY_PREFIX = "This component received deeply equal props";
const PROP_PREFIXES = ["+\u00a0", "\u2013\u00a0"];
const PROP_INDENT = "\u00a0\u00a0";
const UPDATE_KINDS = new Set(["Update", "Cascading Update", "Update Blocked", "Promise Resolved"]);

// Long tasks arrive once the thread unblocks, a little after what ran in them.
const BUFFER_MS = 10_000;
const MAX_FRAMES = 100;
// React development builds emit 50–200 measures a second during streaming.
const MAX_MEASURES = 5_000;
const MAX_SCRIPTS = 8;
const MAX_UPDATES = 8;
const MAX_COMPONENTS = 10;
const MAX_PROPS = 8;
// Long task durations are coarsened to whole milliseconds; measures are not.
const EDGE_SLACK_MS = 1;

let frames: FrameRecord[] = [];
let measures: MeasureRecord[] = [];

interface LoafScript {
  startTime: number;
  duration: number;
  invoker?: string;
  invokerType?: string;
  sourceFunctionName?: string;
  sourceURL?: string;
  sourceCharPosition?: number;
  forcedStyleAndLayoutDuration?: number;
}

interface LoafEntry {
  startTime: number;
  duration: number;
  blockingDuration?: number;
  renderStart?: number;
  styleAndLayoutStart?: number;
  scripts?: readonly LoafScript[];
}

function bounded<T extends Span>(list: T[], item: T, max: number): T[] {
  list.push(item);
  const cutoff = item.end - BUFFER_MS;
  let drop = Math.max(0, list.length - max);
  while (drop < list.length && list[drop]!.end < cutoff) drop += 1;
  return drop > 0 ? list.slice(drop) : list;
}

export function recordFrameEntry(entry: LoafEntry): void {
  const scripts = (entry.scripts ?? []).map((script) => ({
    start: script.startTime,
    end: script.startTime + script.duration,
    invoker: script.invoker || "unknown",
    invokerType: script.invokerType || "unknown",
    functionName: script.sourceFunctionName || null,
    source: script.sourceURL
      ? `${script.sourceURL}${script.sourceCharPosition !== undefined && script.sourceCharPosition >= 0 ? `:${script.sourceCharPosition}` : ""}`
      : null,
    forcedLayoutMs: script.forcedStyleAndLayoutDuration ?? 0,
  }));
  frames = bounded(
    frames,
    {
      start: entry.startTime,
      end: entry.startTime + entry.duration,
      blockingMs: entry.blockingDuration ?? 0,
      renderStart: entry.renderStart ?? 0,
      styleAndLayoutStart: entry.styleAndLayoutStart ?? 0,
      scripts,
    },
    MAX_FRAMES,
  );
}

export function recordMeasureEntry(entry: { name: string; startTime: number; duration: number; detail?: unknown }): void {
  const devtools = (entry.detail as { devtools?: Record<string, unknown> } | null | undefined)?.devtools;
  if (!devtools || typeof devtools !== "object") return;
  const track = typeof devtools.track === "string" ? devtools.track : null;
  const trackGroup = typeof devtools.trackGroup === "string" ? devtools.trackGroup : null;
  const isRender = track === COMPONENTS_TRACK && entry.name.startsWith(COMPONENT_MEASURE_PREFIX);
  const isUpdate = trackGroup === SCHEDULER_TRACK_GROUP && UPDATE_KINDS.has(entry.name);
  if (!isRender && !isUpdate) return;
  measures = bounded(
    measures,
    {
      name: isRender ? entry.name.slice(COMPONENT_MEASURE_PREFIX.length) : entry.name,
      start: entry.startTime,
      end: entry.startTime + entry.duration,
      track,
      trackGroup,
      properties: Array.isArray(devtools.properties) ? devtools.properties : null,
    },
    MAX_MEASURES,
  );
}

function overlaps(a: Span, b: Span): boolean {
  return a.start < b.end && b.start < a.end;
}

function overlap(a: Span, b: Span): number {
  return Math.max(0, Math.min(a.end, b.end) - Math.max(a.start, b.start));
}

function property(properties: readonly unknown[] | null, label: string): string | null {
  for (const entry of properties ?? []) {
    if (Array.isArray(entry) && entry[0] === label && typeof entry[1] === "string" && entry[1]) return entry[1];
  }
  return null;
}

/** Top-level prop names in React's changed-props listing (`+ key` / `– key`, nested keys indented). */
export function changedPropNames(properties: readonly unknown[] | null): string[] {
  const names = new Set<string>();
  for (const entry of properties ?? []) {
    if (!Array.isArray(entry) || typeof entry[0] !== "string") continue;
    const prefix = PROP_PREFIXES.find((p) => (entry[0] as string).startsWith(p));
    if (!prefix) continue;
    const key = entry[0].slice(prefix.length);
    if (!key.startsWith(PROP_INDENT)) names.add(key);
  }
  return [...names];
}

function frameOf(task: Span): FrameRecord | null {
  let best: FrameRecord | null = null;
  for (const frame of frames) {
    if (overlap(frame, task) > (best ? overlap(best, task) : 0)) best = frame;
  }
  return best;
}

/**
 * Self time per measure: its duration less the measures nested directly in
 * it. React logs renders depth-first, so nesting by start and end recovers
 * the tree of the measured components.
 */
function selfTimes(renders: readonly MeasureRecord[]): Map<MeasureRecord, number> {
  const sorted = [...renders].sort((a, b) => a.start - b.start || b.end - a.end);
  const self = new Map<MeasureRecord, number>();
  const stack: MeasureRecord[] = [];
  for (const render of sorted) {
    while (stack.length > 0 && stack.at(-1)!.end <= render.start) stack.pop();
    const parent = stack.at(-1);
    if (parent && render.end <= parent.end) self.set(parent, self.get(parent)! - (render.end - render.start));
    self.set(render, render.end - render.start);
    stack.push(render);
  }
  return self;
}

function componentRenders(renders: readonly MeasureRecord[]): DevComponentRender[] {
  const self = selfTimes(renders);
  const byName = new Map<string, DevComponentRender>();
  for (const render of renders) {
    let summary = byName.get(render.name);
    if (!summary) {
      summary = { name: render.name, renders: 0, selfMs: 0, changedProps: [], deeplyEqualProps: false };
      byName.set(render.name, summary);
    }
    summary.renders += 1;
    summary.selfMs += Math.max(0, self.get(render) ?? 0);
    for (const prop of changedPropNames(render.properties)) {
      if (summary.changedProps.length < MAX_PROPS && !summary.changedProps.includes(prop)) summary.changedProps.push(prop);
    }
    if (property(render.properties, "Changed Props")?.startsWith(DEEP_EQUALITY_PREFIX)) summary.deeplyEqualProps = true;
  }
  return [...byName.values()].sort((a, b) => b.selfMs - a.selfMs).slice(0, MAX_COMPONENTS);
}

/** What the buffers saw during a task, given in performance-timeline milliseconds. */
export function attributeLongTask(startTime: number, durationMs: number): DevLongTaskAttribution {
  const task = { start: startTime, end: startTime + durationMs };
  const frame = frameOf(task);
  const scripts = frames
    .filter((f) => overlaps(f, task))
    .flatMap((f) => f.scripts)
    .filter((script) => overlaps(script, task))
    .sort((a, b) => b.end - b.start - (a.end - a.start))
    .slice(0, MAX_SCRIPTS)
    .map(({ start, end, ...script }) => ({ ...script, durationMs: end - start }));
  const updates = measures
    .filter((m) => m.trackGroup === SCHEDULER_TRACK_GROUP && m.end >= task.start && m.end <= task.end)
    .slice(0, MAX_UPDATES)
    .map((m) => ({
      kind: m.name,
      lane: m.track,
      component: property(m.properties, "Component name"),
      method: property(m.properties, "Method name"),
    }));
  // A task runs without yielding, so a render inside it was rendering the
  // whole time. A render that crosses its edges was time-sliced: React may
  // have yielded to other tasks, this one included, so none of its span is
  // known to be render time here.
  const overlapping = measures.filter((m) => m.track === COMPONENTS_TRACK && overlaps(m, task));
  const inside = (m: MeasureRecord) => m.start >= task.start - EDGE_SLACK_MS && m.end <= task.end + EDGE_SLACK_MS;
  const spanning = new Map<string, number>();
  for (const m of overlapping) if (!inside(m)) spanning.set(m.name, (spanning.get(m.name) ?? 0) + 1);
  return {
    frame: frame && {
      durationMs: frame.end - frame.start,
      blockingMs: frame.blockingMs,
      renderMs: frame.renderStart > 0 ? frame.end - frame.renderStart : 0,
      styleLayoutMs: frame.styleAndLayoutStart > 0 ? frame.end - frame.styleAndLayoutStart : 0,
    },
    scripts,
    updates,
    components: componentRenders(overlapping.filter(inside)),
    spanningComponents: [...spanning]
      .map(([name, renders]) => ({ name, renders }))
      .slice(0, MAX_COMPONENTS),
  };
}

/** Entry types this engine reports that attribution reads, so the recorder observes only those. */
export function attributionEntryTypes(): string[] {
  const supported = PerformanceObserver.supportedEntryTypes ?? [];
  return ["long-animation-frame", "measure"].filter((type) => supported.includes(type));
}

export function resetLongTaskAttribution(): void {
  frames = [];
  measures = [];
}
