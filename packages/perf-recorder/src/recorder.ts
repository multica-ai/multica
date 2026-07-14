import { detectCapabilities } from "./capabilities";
import { subscribeCommit, type CommitRoot } from "./install";
import { isRecorderEvent } from "./self-surface";
import { extractCommitEvidence } from "./react-commit";
import { sanitizeUrl } from "./sanitize-url";
import {
  DEFAULT_THRESHOLDS,
  RECORDER_VERSION,
  SCHEMA_VERSION,
  type Capabilities,
  type EvidenceKind,
  type HostConfig,
  type Incident,
  type InteractionInfo,
  type InteractionType,
  type Report,
  type RouteInfo,
  type Thresholds,
} from "./types";

export type RecorderState = "idle" | "recording" | "stopped";

export interface LiveStatus {
  state: RecorderState;
  fps: number;
  jankCount: number;
  longTaskCount: number;
  durationMs: number;
  incidentCount: number;
}

// A longtask entry is by definition ≥ ~50ms; kept explicit so incident ranking
// can express "how many times over threshold" consistently across signals.
const LONGTASK_THRESHOLD_MS = 50;
const CORRELATION_WINDOW_MS = 1000;
const BACKGROUND_IDLE_MS = 1000;
const MAX_INCIDENTS = 500;
const MAX_EVIDENCE_PER_INCIDENT = 200;

interface InternalIncident extends Incident {
  _startTime: number;
  _endTime: number;
  _routeKey: string;
}

type StatusListener = (status: LiveStatus) => void;
type IncidentsListener = (incidents: Incident[]) => void;

export class Recorder {
  private readonly appVersion: string;
  private readonly surface: HostConfig["surface"];
  private readonly mode: HostConfig["mode"];
  private readonly thresholds: Thresholds;
  private readonly boundaryAllowlist: ReadonlySet<string>;
  private readonly testIdAllowlist: ReadonlySet<string>;

  private state: RecorderState = "idle";
  private capabilities: Capabilities;
  private sessionStart = 0;
  private sessionEnd = 0;

  private incidents: InternalIncident[] = [];
  private current: InternalIncident | null = null;
  private currentWindowEnd = 0;
  private background: InternalIncident | null = null;
  private backgroundIdleUntil = 0;
  private incidentSeq = 0;

  // live status counters
  private jankCount = 0;
  private longTaskCount = 0;
  private frameTicks = 0;
  private lastFpsSample = 0;
  private fps = 0;

  // teardown handles
  private observers: PerformanceObserver[] = [];
  private mutationObserver: MutationObserver | null = null;
  private eventCleanups: Array<() => void> = [];
  private rafHandle: number | null = null;
  private unsubscribeCommit: (() => void) | null = null;

  private statusListeners = new Set<StatusListener>();
  private incidentsListeners = new Set<IncidentsListener>();

  constructor(config: HostConfig) {
    this.appVersion = config.appVersion;
    this.surface = config.surface;
    this.mode = config.mode;
    this.thresholds = { ...DEFAULT_THRESHOLDS, ...(config.thresholds ?? {}) };
    this.boundaryAllowlist = new Set(config.boundaryAllowlist ?? []);
    this.testIdAllowlist = new Set(config.testIdAllowlist ?? []);
    this.capabilities = detectCapabilities(false);
  }

  getState(): RecorderState {
    return this.state;
  }

  onStatus(listener: StatusListener): () => void {
    this.statusListeners.add(listener);
    return () => this.statusListeners.delete(listener);
  }

  onIncidents(listener: IncidentsListener): () => void {
    this.incidentsListeners.add(listener);
    return () => this.incidentsListeners.delete(listener);
  }

  start(): void {
    if (this.state === "recording") return;
    // Fresh session: drop any prior un-exported in-memory data.
    this.reset();
    this.state = "recording";
    this.sessionStart = now();
    this.lastFpsSample = this.sessionStart;
    this.capabilities = detectCapabilities(true);
    this.setupCollectors();
    this.emitStatus();
    this.emitIncidents();
  }

  stop(): void {
    if (this.state !== "recording") return;
    this.teardownCollectors();
    this.finalizeAll();
    this.sessionEnd = now();
    this.state = "stopped";
    this.emitStatus();
    this.emitIncidents();
  }

  clear(): void {
    this.teardownCollectors();
    this.reset();
    this.state = "idle";
    this.emitStatus();
    this.emitIncidents();
  }

  /** RFC §7: a report may only be exported once the session is stopped. */
  canExport(): boolean {
    return this.state === "stopped";
  }

  export(): Report {
    // RFC §7: export is only available in the stopped state. Enforced at the
    // recorder layer so a programmatic caller can't dump a mid-recording report;
    // the panel additionally disables/no-ops the Export control until stopped.
    if (this.state !== "stopped") {
      throw new Error("perf-recorder: export() is only available after stop() (RFC §7)");
    }
    this.finalizeAll();
    const incidents = this.incidents.map(stripInternal);
    return {
      schemaVersion: SCHEMA_VERSION,
      recorderVersion: RECORDER_VERSION,
      host: { appVersion: this.appVersion, surface: this.surface, mode: this.mode },
      capabilities: this.capabilities,
      thresholdsMs: this.thresholds,
      session: { durationMs: this.sessionDurationMs(), incidentCount: incidents.length },
      incidents,
    };
  }

  private sessionDurationMs(): number {
    if (!this.sessionStart) return 0;
    const end = this.sessionEnd || now();
    return Math.max(0, Math.round(end - this.sessionStart));
  }

  getIncidents(): Incident[] {
    return this.incidents.map(stripInternal);
  }

  getCapabilities(): Capabilities {
    return this.capabilities;
  }

  // ---- collectors ----------------------------------------------------------

  private setupCollectors(): void {
    this.observePerformance("longtask", (entry) => {
      this.longTaskCount++;
      this.jankCount++;
      this.attach(
        "long_task",
        entry.startTime,
        entry.startTime + entry.duration,
        (incident) => pushCapped(incident.longTasks, {
          startOffsetMs: this.offset(entry.startTime),
          durationMs: round(entry.duration),
        }),
      );
    });

    if (this.capabilities.longAnimationFrame) {
      this.observePerformance("long-animation-frame", (entry) => {
        this.jankCount++;
        this.attach("frame", entry.startTime, entry.startTime + entry.duration, (incident) =>
          pushCapped(incident.frames, {
            startOffsetMs: this.offset(entry.startTime),
            durationMs: round(entry.duration),
            source: "loaf",
          }),
        );
      });
    }

    if (this.capabilities.resourceTiming) {
      this.observePerformance("resource", (entry) => {
        const res = entry as PerformanceResourceTiming;
        if (res.duration < this.thresholds.resourceMs) return;
        const route = sanitizeUrl(res.name, docBaseUrl());
        this.attach("resource", res.startTime, res.startTime + res.duration, (incident) =>
          pushCapped(incident.resources, {
            origin: route.origin,
            pathname: route.pathname,
            initiatorType: typeof res.initiatorType === "string" ? res.initiatorType : "other",
            durationMs: round(res.duration),
            startOffsetMs: this.offset(res.startTime),
          }),
        );
      });
    }

    if (this.capabilities.reactCommit) {
      this.unsubscribeCommit = subscribeCommit((root) => this.onCommit(root));
    }

    if (this.capabilities.mutationObserver && typeof document !== "undefined") {
      this.mutationObserver = new MutationObserver((records) => {
        // Only the COUNT is used. MutationRecord contents (added/removed nodes,
        // attribute names/values, subtrees, HTML) are never read (MUL-4466 §12).
        const count = records.length;
        this.attach("interaction", now(), now(), (incident) => {
          incident.mutationCount += count;
        });
      });
      this.mutationObserver.observe(document.documentElement, {
        childList: true,
        subtree: true,
        attributes: true,
      });
    }

    this.installInteractionListeners();
    this.startFrameLoop();
  }

  private observePerformance(
    type: string,
    onEntry: (entry: PerformanceEntry) => void,
  ): void {
    if (typeof PerformanceObserver === "undefined") return;
    try {
      const observer = new PerformanceObserver((list) => {
        for (const entry of list.getEntries()) {
          try {
            onEntry(entry);
          } catch {
            /* fail-open */
          }
        }
      });
      observer.observe({ type, buffered: false } as PerformanceObserverInit);
      this.observers.push(observer);
    } catch {
      /* entry type unsupported at observe time — capability already flagged */
    }
  }

  private installInteractionListeners(): void {
    if (typeof window === "undefined") return;
    const handler = (type: InteractionType) => (event: Event) => {
      // Exclude the recorder's own surface: a click/scroll/keydown inside the
      // panel's Shadow root still retargets and bubbles to window, so without
      // this the panel's own buttons would be logged as app interactions
      // (MUL-4466 §10.2).
      if (isRecorderEvent(event)) return;
      const testId = this.resolveTestId(event.target);
      this.openInteraction(type, testId);
    };
    const add = (name: string, type: InteractionType) => {
      const fn = handler(type);
      // capture phase so we open the window before app handlers run
      window.addEventListener(name, fn, { capture: true, passive: true });
      this.eventCleanups.push(() => window.removeEventListener(name, fn, { capture: true }));
    };
    add("click", "click");
    add("keydown", "input");
    add("scroll", "scroll");
    // route changes: history API + popstate
    const onNav = () => this.openInteraction("navigation", undefined);
    window.addEventListener("popstate", onNav);
    this.eventCleanups.push(() => window.removeEventListener("popstate", onNav));
  }

  private startFrameLoop(): void {
    if (typeof requestAnimationFrame === "undefined") return;
    let last = now();
    const tick = () => {
      const t = now();
      const gap = t - last;
      this.frameTicks++;
      // rAF gap fallback for dropped frames when LoAF is unavailable.
      if (!this.capabilities.longAnimationFrame && gap >= this.thresholds.frameGapMs) {
        this.jankCount++;
        this.attach("frame", last, t, (incident) =>
          pushCapped(incident.frames, {
            startOffsetMs: this.offset(last),
            durationMs: round(gap),
            source: "raf",
          }),
        );
      }
      // sample fps ~1Hz
      if (t - this.lastFpsSample >= 1000) {
        this.fps = Math.round((this.frameTicks * 1000) / (t - this.lastFpsSample));
        this.frameTicks = 0;
        this.lastFpsSample = t;
        this.emitStatus();
      }
      last = t;
      this.rafHandle = requestAnimationFrame(tick);
    };
    this.rafHandle = requestAnimationFrame(tick);
  }

  private onCommit(root: CommitRoot): void {
    const { commitActualDurationMs, phase, boundaries } = extractCommitEvidence(
      root,
      this.boundaryAllowlist,
    );
    const registered = boundaries.filter((b) => b.actualDurationMs >= this.thresholds.reactCommitMs);
    const anonymousSlow =
      commitActualDurationMs !== null && commitActualDurationMs >= this.thresholds.reactCommitMs;
    if (registered.length === 0 && !anonymousSlow) return;
    this.jankCount++;
    const t = now();
    this.attach("react_commit", t, t, (incident) => {
      if (registered.length > 0) {
        for (const b of registered) pushCapped(incident.reactCommits, b);
      } else if (anonymousSlow) {
        pushCapped(incident.reactCommits, {
          phase,
          actualDurationMs: round(commitActualDurationMs as number),
        });
      }
    });
  }

  // ---- correlation ---------------------------------------------------------

  private openInteraction(type: InteractionType, testId: string | undefined): void {
    if (this.state !== "recording") return;
    const t = now();
    const route = this.route();
    const interaction: InteractionInfo = testId ? { type, testId } : { type };
    const incident = this.newIncident(route, interaction, t);
    this.current = incident;
    this.currentWindowEnd = t + CORRELATION_WINDOW_MS;
    this.pushIncident(incident);
    this.emitIncidents();
  }

  /** Route the evidence to the interaction window if fresh & same-route, else background. */
  private attach(
    _kind: EvidenceKind,
    startTime: number,
    endTime: number,
    apply: (incident: InternalIncident) => void,
  ): void {
    if (this.state !== "recording") return;
    const t = now();
    const routeKey = this.route().pathname ?? "";
    let incident: InternalIncident;
    if (this.current && t <= this.currentWindowEnd && this.current._routeKey === routeKey) {
      incident = this.current;
    } else {
      incident = this.backgroundIncident(routeKey, t);
    }
    apply(incident);
    incident._startTime = Math.min(incident._startTime, startTime);
    incident._endTime = Math.max(incident._endTime, endTime);
    this.emitIncidents();
  }

  private backgroundIncident(routeKey: string, t: number): InternalIncident {
    if (this.background && t <= this.backgroundIdleUntil && this.background._routeKey === routeKey) {
      this.backgroundIdleUntil = t + BACKGROUND_IDLE_MS;
      return this.background;
    }
    const incident = this.newIncident(this.route(), { type: "background" }, t);
    this.background = incident;
    this.backgroundIdleUntil = t + BACKGROUND_IDLE_MS;
    this.pushIncident(incident);
    return incident;
  }

  private newIncident(route: RouteInfo, interaction: InteractionInfo, t: number): InternalIncident {
    return {
      id: `local-${++this.incidentSeq}`,
      offsetMs: this.offset(t),
      route,
      interaction,
      totalDurationMs: 0,
      primaryEvidence: "insufficient",
      mutationCount: 0,
      reactCommits: [],
      longTasks: [],
      frames: [],
      resources: [],
      _startTime: t,
      _endTime: t,
      _routeKey: route.pathname ?? "",
    };
  }

  private pushIncident(incident: InternalIncident): void {
    this.incidents.push(incident);
    if (this.incidents.length > MAX_INCIDENTS) {
      const dropped = this.incidents.shift();
      if (dropped === this.current) this.current = null;
      if (dropped === this.background) this.background = null;
    }
  }

  private finalizeAll(): void {
    for (const incident of this.incidents) this.finalize(incident);
  }

  private finalize(incident: InternalIncident): void {
    const span = incident._endTime - incident._startTime;
    incident.totalDurationMs = Math.max(0, round(span));
    incident.primaryEvidence = this.rankPrimary(incident);
  }

  private rankPrimary(incident: Incident): EvidenceKind {
    const ratios: Array<[EvidenceKind, number]> = [];
    const maxCommit = maxBy(incident.reactCommits, (c) => c.actualDurationMs);
    if (maxCommit > 0) ratios.push(["react_commit", maxCommit / this.thresholds.reactCommitMs]);
    const maxTask = maxBy(incident.longTasks, (l) => l.durationMs);
    if (maxTask > 0) ratios.push(["long_task", maxTask / LONGTASK_THRESHOLD_MS]);
    const maxFrame = maxBy(incident.frames, (f) => f.durationMs);
    if (maxFrame > 0) ratios.push(["frame", maxFrame / this.thresholds.frameGapMs]);
    const maxRes = maxBy(incident.resources, (r) => r.durationMs);
    if (maxRes > 0) ratios.push(["resource", maxRes / this.thresholds.resourceMs]);
    if (ratios.length === 0) return "insufficient";
    ratios.sort((a, b) => b[1] - a[1]);
    // Only claim a root cause when at least one signal exceeded its threshold.
    return ratios[0]![1] >= 1 ? ratios[0]![0] : "insufficient";
  }

  private resolveTestId(target: EventTarget | null): string | undefined {
    if (this.testIdAllowlist.size === 0) return undefined;
    let node = target as Element | null;
    let depth = 0;
    while (node && depth < 20) {
      if (typeof node.getAttribute === "function") {
        const id = node.getAttribute("data-testid");
        if (id && this.testIdAllowlist.has(id)) return id;
      }
      node = node.parentElement;
      depth++;
    }
    return undefined;
  }

  private route(): RouteInfo {
    if (typeof location === "undefined") return { origin: null, pathname: null };
    return sanitizeUrl(location.href);
  }

  private offset(t: number): number {
    return Math.max(0, round(t - this.sessionStart));
  }

  private teardownCollectors(): void {
    for (const observer of this.observers) {
      try {
        observer.disconnect();
      } catch {
        /* ignore */
      }
    }
    this.observers = [];
    this.mutationObserver?.disconnect();
    this.mutationObserver = null;
    for (const cleanup of this.eventCleanups) cleanup();
    this.eventCleanups = [];
    if (this.rafHandle !== null && typeof cancelAnimationFrame !== "undefined") {
      cancelAnimationFrame(this.rafHandle);
    }
    this.rafHandle = null;
    this.unsubscribeCommit?.();
    this.unsubscribeCommit = null;
  }

  private reset(): void {
    this.incidents = [];
    this.current = null;
    this.background = null;
    this.currentWindowEnd = 0;
    this.backgroundIdleUntil = 0;
    this.incidentSeq = 0;
    this.jankCount = 0;
    this.longTaskCount = 0;
    this.frameTicks = 0;
    this.fps = 0;
    this.sessionStart = 0;
    this.sessionEnd = 0;
  }

  private emitStatus(): void {
    const status: LiveStatus = {
      state: this.state,
      fps: this.fps,
      jankCount: this.jankCount,
      longTaskCount: this.longTaskCount,
      durationMs:
        this.state === "recording" ? round(now() - this.sessionStart) : this.sessionDurationMs(),
      incidentCount: this.incidents.length,
    };
    for (const listener of this.statusListeners) {
      try {
        listener(status);
      } catch {
        /* ignore */
      }
    }
  }

  private emitIncidents(): void {
    const snapshot = this.incidents.map(stripInternal);
    for (const listener of this.incidentsListeners) {
      try {
        listener(snapshot);
      } catch {
        /* ignore */
      }
    }
  }
}

// ---- helpers ---------------------------------------------------------------

function now(): number {
  return typeof performance !== "undefined" ? performance.now() : Date.now();
}

function docBaseUrl(): string | undefined {
  return typeof document !== "undefined" ? document.baseURI : undefined;
}

function round(n: number): number {
  return Math.round(n * 10) / 10;
}

function maxBy<T>(items: T[], pick: (item: T) => number): number {
  let max = 0;
  for (const item of items) {
    const v = pick(item);
    if (v > max) max = v;
  }
  return max;
}

function pushCapped<T>(arr: T[], item: T): void {
  if (arr.length < MAX_EVIDENCE_PER_INCIDENT) arr.push(item);
}

function stripInternal(incident: InternalIncident): Incident {
  const { _startTime, _endTime, _routeKey, ...rest } = incident;
  void _startTime;
  void _endTime;
  void _routeKey;
  return rest;
}
