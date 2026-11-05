import { reaction } from "mobx";
import type { AgentGuiHostServices, AgentGuiInitSnapshot } from "./protocol";
import type { createMulticaAgentGuiRuntime } from "./runtime";

const WINDOW_MS = 2_000;
const PREFIX = "[ui-performance] ";
type Action = "first_send" | "switch_conversation";
type Outcome = "window_complete" | "superseded" | "hidden" | "unmounted" | "stopped";
interface SlowScript {
  file: string;
  functionName: string;
  sourceCharPosition: number;
  durationMs: number;
  forcedStyleAndLayoutMs: number;
}
interface LongAnimationFrame extends PerformanceEntry {
  renderStart: number;
  blockingDuration: number;
  scripts: (PerformanceEntry & { sourceURL: string; sourceFunctionName: string;
    sourceCharPosition: number; forcedStyleAndLayoutDuration: number })[];
}

interface UiSample {
  action: Action;
  surface: "main" | "side";
  at: string;
  outcome: Outcome;
  durationMs: number;
  sampledFrames: number;
  maxFrameIntervalMs: number;
  frameIntervalsOver50Ms: number;
  reactCommits: number;
  reactDurationMs: number;
  maxReactDurationMs: number;
  selectedSessionFrameMs: number | null;
  firstMessageFrameMs: number | null;
  messageDisappearances: number;
  welcomeReentries: number;
  composerReplacements: number;
  browserEntryTypes: string[];
  maxEventResponseMs: number | null;
  pageLongTasks: number;
  pageLongTaskMs: number;
  pageLongAnimationFrames: number;
  pageAnimationBlockingMs: number;
  pageRenderPhaseMs: number;
  slowScripts: SlowScript[];
  layoutShiftScore: number;
}

// This is a bounded, in-memory development capture. Never retain DOM, URLs or message content.
const samples: UiSample[] = [];
const stopCaptures = new Set<() => void>();
let enabled = true;
declare global {
  interface Window {
    multicaUiPerformance?: {
      start(): void;
      stop(): void;
      snapshot(): UiSample[];
    };
  }
}

/** Called only from the development mount; ordinary renders do not start frame sampling. */
export function installConversationUiProfile(
  root: HTMLElement,
  client: AgentGuiHostServices,
  handle: ReturnType<typeof createMulticaAgentGuiRuntime>,
  initial: AgentGuiInitSnapshot,
) {
  let visible = initial.hostVisible !== false;
  const browserEntryTypes = typeof PerformanceObserver === "undefined" ? [] :
    ["longtask", "layout-shift", "event", "long-animation-frame"].filter((type) => PerformanceObserver.supportedEntryTypes.includes(type));
  let capture: { sample: UiSample; sessionId: string; started: number; lastFrame: number | null;
    userMessage: boolean; welcome: boolean; editor: Element | null } | undefined;
  let frame = 0;
  let timeout: ReturnType<typeof setTimeout> | undefined;
  const now = () => performance.now();
  const round = (value: number) => Math.round(value * 100) / 100;
  const finish = (outcome: Outcome) => {
    if (!capture) return;
    cancelAnimationFrame(frame);
    clearTimeout(timeout);
    const result = { ...capture.sample, outcome, durationMs: round(now() - capture.started) };
    capture = undefined;
    samples.push(result);
    if (samples.length > 100) samples.shift();
    console.info(PREFIX + JSON.stringify(result));
  };
  const tick = (time: number) => {
    if (!capture) return;
    if (!visible || document.visibilityState !== "visible") { finish("hidden"); return; }
    const { sample } = capture;
    const interval = capture.lastFrame === null ? 0 : time - capture.lastFrame;
    capture.lastFrame = time;
    sample.sampledFrames++;
    sample.maxFrameIntervalMs = Math.max(sample.maxFrameIntervalMs, round(interval));
    if (interval > 50) sample.frameIntervalsOver50Ms++;
    const pane = root.querySelector("[data-agent-session-id]");
    if (pane?.getAttribute("data-agent-session-id") === capture.sessionId) {
      sample.selectedSessionFrameMs ??= round(time - capture.started);
      const userMessage = !!pane.querySelector('[data-agent-message-speaker="user"]');
      const welcome = !!pane.querySelector(".multica-empty-conversation-welcome");
      if (sample.action === "first_send") {
        if (userMessage) sample.firstMessageFrameMs ??= round(time - capture.started);
        if (capture.userMessage && !userMessage) sample.messageDisappearances++;
        if (sample.firstMessageFrameMs !== null && welcome && !capture.welcome) sample.welcomeReentries++;
      }
      capture.userMessage = userMessage;
      capture.welcome = welcome;
    }
    const editor = root.querySelector('[contenteditable="true"]');
    if (editor) {
      if (capture.editor && capture.editor !== editor) sample.composerReplacements++;
      capture.editor = editor;
    }
    frame = requestAnimationFrame(tick);
  };
  const begin = (action: Action, sessionId: string, started = now()) => {
    if (!enabled || !visible || document.visibilityState !== "visible") return;
    finish("superseded");
    capture = {
      sessionId, started, lastFrame: null, userMessage: false, welcome: false,
      editor: root.querySelector('[contenteditable="true"]'),
      sample: {
        action, surface: initial.sidePane ? "side" : "main", at: new Date().toISOString(),
        outcome: "window_complete", durationMs: 0, sampledFrames: 0, maxFrameIntervalMs: 0,
        frameIntervalsOver50Ms: 0, reactCommits: 0, reactDurationMs: 0, maxReactDurationMs: 0,
        selectedSessionFrameMs: null, firstMessageFrameMs: null, messageDisappearances: 0,
        welcomeReentries: 0, composerReplacements: 0, browserEntryTypes, maxEventResponseMs: null,
        pageLongTasks: 0, pageLongTaskMs: 0, layoutShiftScore: 0,
        pageLongAnimationFrames: 0, pageAnimationBlockingMs: 0, pageRenderPhaseMs: 0, slowScripts: [],
      },
    };
    frame = requestAnimationFrame(tick);
    timeout = setTimeout(() => finish("window_complete"), WINDOW_MS);
  };
  const stop = () => finish("stopped");
  stopCaptures.add(stop);
  window.multicaUiPerformance = {
    start: () => { samples.length = 0; enabled = true; },
    stop: () => { enabled = false; for (const stop of stopCaptures) stop(); },
    snapshot: () => samples.map((sample) => ({ ...sample, browserEntryTypes: [...sample.browserEntryTypes], slowScripts: sample.slowScripts.map((script) => ({ ...script })) })),
  };
  const known = new Set(handle.runtime.activations.pending().map((item) => item.requestId));
  const stopActivation = reaction(
    () => handle.runtime.activations.pending(),
    (activations) => {
      for (const activation of activations) {
        if (known.has(activation.requestId)) continue;
        known.add(activation.requestId);
        if (activation.mode === "new" && activation.content.length)
          begin("first_send", activation.agentSessionId,
            now() - Math.max(0, Date.now() - activation.requestedAtUnixMs));
      }
    },
  );
  const click = (event: MouseEvent) => {
    if (initial.sidePane || event.button !== 0 || event.metaKey || event.ctrlKey || event.shiftKey || event.altKey) return;
    const link = event.target instanceof Element ? event.target.closest<HTMLAnchorElement>("#local-sessions a[href]") : null;
    if (!link || link.getAttribute("aria-current") === "true") return;
    const id = new URL(link.href).searchParams.get("session");
    if (id) begin("switch_conversation", id, event.timeStamp);
  };
  document.addEventListener("click", click, true);
  const visibility = () => { if (document.visibilityState !== "visible") finish("hidden"); };
  document.addEventListener("visibilitychange", visibility);
  const unsubscribe = client.subscribe((event) => {
    if (event.type === "init" && event.snapshot.workspaceId === initial.workspaceId && event.snapshot.userId === initial.userId) {
      visible = event.snapshot.hostVisible !== false;
      if (!visible) finish("hidden");
    }
  });
  const observers: PerformanceObserver[] = [];
  for (const type of browserEntryTypes) {
    const observer = new PerformanceObserver((list) => {
      for (const entry of list.getEntries()) {
        if (!capture || (entry.entryType === "long-animation-frame"
          ? entry.startTime + entry.duration < capture.started : entry.startTime < capture.started)) continue;
        const { sample } = capture;
        if (entry.entryType === "longtask") {
          // Long tasks are renderer-wide; do not attribute them to one React component.
          sample.pageLongTasks++;
          sample.pageLongTaskMs = round(sample.pageLongTaskMs + entry.duration);
        } else if (entry.entryType === "long-animation-frame") {
          const animation = entry as LongAnimationFrame;
          sample.pageLongAnimationFrames++;
          sample.pageAnimationBlockingMs = round(sample.pageAnimationBlockingMs + animation.blockingDuration);
          if (animation.renderStart > 0)
            sample.pageRenderPhaseMs = round(sample.pageRenderPhaseMs + animation.startTime + animation.duration - animation.renderStart);
          for (const script of animation.scripts) {
            // Keep repository-relative module names only, never origins, query strings or private paths.
            const file = /\/(packages|vendor|apps|src|node_modules)\/[^?#]+/.exec(script.sourceURL)?.[0].slice(1) ?? "unattributed";
            sample.slowScripts.push({ file, functionName: script.sourceFunctionName.slice(0, 100),
              sourceCharPosition: script.sourceCharPosition, durationMs: round(script.duration),
              forcedStyleAndLayoutMs: round(script.forcedStyleAndLayoutDuration) });
          }
          sample.slowScripts.sort((a, b) => b.durationMs - a.durationMs);
          sample.slowScripts.length = Math.min(sample.slowScripts.length, 5);
        } else if (entry.entryType === "layout-shift") {
          const shift = entry as PerformanceEntry & { value: number; sources?: { node?: Node }[] };
          if (shift.sources?.some(({ node }) => node && root.contains(node)))
            sample.layoutShiftScore = Math.round((sample.layoutShiftScore + shift.value) * 1_000_000) / 1_000_000;
        } else {
          const timing = entry as PerformanceEventTiming;
          if (timing.target && (root.contains(timing.target) ||
            (timing.target instanceof Element && timing.target.closest("#local-sessions"))))
            sample.maxEventResponseMs = Math.max(sample.maxEventResponseMs ?? 0, timing.duration);
        }
      }
    });
    observer.observe({ type, durationThreshold: 16 } as PerformanceObserverInit & { durationThreshold: number });
    observers.push(observer);
  }
  console.info(PREFIX + JSON.stringify({ event: "ready", surface: initial.sidePane ? "side" : "main", windowMs: WINDOW_MS, browserEntryTypes }));
  return {
    rendered(duration: number) {
      if (!capture) return;
      capture.sample.reactCommits++;
      capture.sample.reactDurationMs = round(capture.sample.reactDurationMs + duration);
      capture.sample.maxReactDurationMs = Math.max(capture.sample.maxReactDurationMs, round(duration));
    },
    dispose() {
      finish("unmounted");
      stopCaptures.delete(stop);
      stopActivation();
      unsubscribe();
      document.removeEventListener("click", click, true);
      document.removeEventListener("visibilitychange", visibility);
      for (const observer of observers) observer.disconnect();
    },
  };
}
