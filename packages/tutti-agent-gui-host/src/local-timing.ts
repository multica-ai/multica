import { localTimingFailure, parseLocalTiming, type LocalTimingFailure, type LocalTimingIdentity, type LocalTimingOutcome, type LocalTimingRecord, type LocalTimingStage } from "@multica/core/local-conversation/timing";

export * from "@multica/core/local-conversation/timing";

/** Diagnostics contain identifiers and timings only, never prompts or provider payloads. */
export const LOCAL_TIMING_PREFIX = "[local-turn-timing] ";
export type LocalTimingSink = (record: LocalTimingRecord) => void;

export const consoleLocalTiming: LocalTimingSink = (record) => {
  console.info(LOCAL_TIMING_PREFIX + JSON.stringify(record));
};

/** `startedAt` dates the span back to a wall-clock moment before its conversation was known. */
export function createLocalTiming(identity: LocalTimingIdentity, sink: LocalTimingSink = consoleLocalTiming, startedAt?: number) {
  const start = performance.now() - (startedAt === undefined ? 0 : Date.now() - startedAt);
  const seen = new Set<LocalTimingStage>();
  const tools = new Map<string, number>();
  const active: { stage: LocalTimingStage }[] = [];
  let lastActivity = start;
  let phase: LocalTimingStage = "turn.execute";
  let closed = false;
  let timer: ReturnType<typeof setInterval> | undefined;
  const write = (stage: LocalTimingStage, event: LocalTimingRecord["event"], extra: Partial<Pick<LocalTimingRecord, "durationMs" | "idleMs" | "outcome" | "failure">> = {}, background = false) => {
    if (closed && !background) return;
    try {
      const record = parseLocalTiming({ ...identity, version: 2, at: new Date().toISOString(), stage, event, elapsedMs: Math.round(performance.now() - start), ...(background ? { background: true } : {}), ...extra });
      if (record) sink(record);
    }
    catch { /* Diagnostics must not change execution or delivery. */ }
  };
  const span = async <T>(stage: LocalTimingStage, action: () => Promise<T>, background = false): Promise<T> => {
    const pending = { stage };
    if (!background) { phase = stage; active.push(pending); }
    const before = performance.now();
    write(stage, "start", {}, background);
    let outcome: LocalTimingOutcome = "completed";
    let failure: LocalTimingFailure | undefined;
    try { return await action(); }
    catch (error) { failure = localTimingFailure(error); outcome = failure === "cancelled" ? "cancelled" : "failed"; throw error; }
    finally {
      if (!background) active.splice(active.indexOf(pending), 1);
      write(stage, "end", { durationMs: Math.round(performance.now() - before), outcome, ...(failure ? { failure } : {}) }, background);
    }
  };
  return {
    markStart(stage: LocalTimingStage) {
      try {
        const record = parseLocalTiming({ ...identity, version: 2, at: new Date(startedAt ?? Date.now()).toISOString(), stage, event: "mark", elapsedMs: 0 });
        if (record) sink(record);
      }
      catch { /* Diagnostics must not change execution or delivery. */ }
    },
    mark(stage: LocalTimingStage, extra?: Partial<Pick<LocalTimingRecord, "durationMs" | "outcome" | "failure">>) {
      phase = stage; lastActivity = performance.now(); write(stage, "mark", extra);
    },
    once(stage: LocalTimingStage) { if (!seen.has(stage)) { seen.add(stage); this.mark(stage); } },
    activity() { lastActivity = performance.now(); },
    tool(type: "tool_use" | "tool_result", id: string | undefined) {
      if (closed || !id) return;
      phase = "provider.tool"; lastActivity = performance.now();
      if (type === "tool_use") {
        if (tools.has(id) || tools.size >= 512) return;
        tools.set(id, performance.now()); write(phase, "start");
      } else {
        const began = tools.get(id); tools.delete(id);
        write(phase, "end", began === undefined ? {} : { durationMs: Math.round(performance.now() - began) });
      }
    },
    measure<T>(stage: LocalTimingStage, action: () => Promise<T>): Promise<T> { return span(stage, action); },
    // Cleanup spans remain observable after foreground completion without extending its heartbeat.
    background<T>(stage: LocalTimingStage, action: () => Promise<T>): Promise<T> { return span(stage, action, true); },
    heartbeat() {
      if (timer || closed) return;
      timer = setInterval(() => write(active.at(-1)?.stage ?? phase, "wait", { idleMs: Math.round(performance.now() - lastActivity) }), 15_000);
    },
    close() { if (timer) clearInterval(timer); closed = true; },
    finish(outcome: LocalTimingOutcome) {
      write("turn.finished", "end", { outcome }); this.close();
    },
  };
}
export type LocalTiming = ReturnType<typeof createLocalTiming>;

/** One attempt at sending a message: a retry keeps its request id and counts as another attempt. */
export function createLocalSendTiming(sink: LocalTimingSink = consoleLocalTiming) {
  const attempts = new Map<string, number>();
  return {
    begin(conversationId: string, requestId: string, startedAt: number): LocalTiming {
      const attempt = (attempts.get(requestId) ?? 0) + 1;
      attempts.set(requestId, attempt);
      if (attempts.size > 512) attempts.delete(attempts.keys().next().value!);
      const timing = createLocalTiming({ source: "gui", clientKind: "desktop", conversationId, requestId, attempt }, sink, startedAt);
      timing.markStart("client.send");
      // The engine shows the pending message before this runs, so the next frame paints it.
      if (typeof requestAnimationFrame === "function") requestAnimationFrame(() => timing.mark("client.pending_shown"));
      return timing;
    },
  };
}

/** Observe live turns only. Loading old history must not look like a new reply. */
export function createLocalPresentationTiming(sink: LocalTimingSink = consoleLocalTiming) {
  interface Turn { timing: LocalTiming; received: boolean; output: boolean; terminal: boolean }
  const turns = new Map<string, Turn>();
  const open = (conversationId: string, turnId: string): Turn => {
    let turn = turns.get(turnId);
    if (!turn) {
      turn = { timing: createLocalTiming({ source: "gui", clientKind: "desktop", conversationId, turnId }, sink), received: false, output: false, terminal: false };
      turns.set(turnId, turn);
      if (turns.size > 512) turns.delete(turns.keys().next().value!);
    }
    return turn;
  };
  return {
    /** An event carrying model output reached this client; only live delivery calls this, never a history read. */
    received(conversationId: string, turnId: string) {
      const turn = open(conversationId, turnId);
      if (!turn.received) { turn.received = true; turn.timing.mark("client.first_event_received"); }
    },
    applied(conversationId: string, turnId: string, hasOutput: boolean, terminal: boolean) {
      if (!turns.has(turnId) && terminal) return;
      const turn = open(conversationId, turnId);
      if (hasOutput && !turn.output) { turn.output = true; turn.timing.mark("client.first_event_applied"); }
      if (terminal && !turn.terminal) { turn.terminal = true; turn.timing.mark("client.terminal_applied"); }
    },
  };
}

const LOCAL_COST_WINDOW_MS = 5_000;
const LOCAL_COST_MARKS = { projection: "client.projection_summary", dispatch: "client.dispatch_summary", render: "client.render_summary" } as const;
export type LocalCostSegment = keyof typeof LOCAL_COST_MARKS;

/**
 * What applying a conversation's changes costs on this thread. These run for
 * every batch of a stream, too often for a record each, so each window writes one
 * summary per segment. Records carry timings and counts only.
 */
export function createLocalCostProfile(options: { itemCount(conversationId: string): number; sink?: LocalTimingSink; heapBytes?(): number | undefined }) {
  const sink = options.sink ?? consoleLocalTiming;
  const heapBytes = options.heapBytes ?? (() => (performance as { memory?: { usedJSHeapSize?: number } }).memory?.usedJSHeapSize);
  const samples = new Map<LocalCostSegment, { count: number; total: number; longest: number }>();
  let conversationId: string | null = null;
  let started = 0;
  let timer: ReturnType<typeof setTimeout> | undefined;
  const write = (stage: LocalTimingStage, id: string, fields: Partial<LocalTimingRecord>) => {
    try {
      const record = parseLocalTiming({ source: "gui", clientKind: "desktop", conversationId: id, version: 2, at: new Date().toISOString(), stage, event: "mark", elapsedMs: Math.round(performance.now() - started), ...fields });
      if (record) sink(record);
    } catch { /* Diagnostics must not change execution or delivery. */ }
  };
  const flush = () => {
    if (timer !== undefined) clearTimeout(timer);
    timer = undefined;
    const id = conversationId;
    if (id === null || !samples.size) return;
    for (const [segment, sample] of samples) {
      write(LOCAL_COST_MARKS[segment], id, { count: sample.count, durationMs: Math.round(sample.total), maxGapMs: Math.round(sample.longest) });
    }
    samples.clear();
    const bytes = heapBytes();
    write("client.heap_sample", id, { count: options.itemCount(id), ...(bytes === undefined ? {} : { bytes }) });
  };
  const record = (segment: LocalCostSegment, id: string, ms: number) => {
    if (conversationId !== id) { flush(); conversationId = id; }
    if (!samples.size) started = performance.now();
    const sample = samples.get(segment) ?? { count: 0, total: 0, longest: 0 };
    sample.count++; sample.total += ms; sample.longest = Math.max(sample.longest, ms);
    samples.set(segment, sample);
    timer ??= setTimeout(flush, LOCAL_COST_WINDOW_MS);
  };
  return {
    /** Runs one segment and adds its duration to the conversation's window. */
    measure<T>(segment: LocalCostSegment, id: string, action: () => T): T {
      const before = performance.now();
      try { return action(); } finally { record(segment, id, performance.now() - before); }
    },
    /** Adds a React commit, which applies to whichever conversation changed last. */
    rendered(ms: number) { if (conversationId !== null) record("render", conversationId, ms); },
    dispose: flush,
  };
}
