/**
 * Shared in-page measurement for the core-path benchmark (MUL-9539).
 *
 * Two numbers matter to the people reporting slowness: how long until the
 * content they asked for is on screen, and how badly the page stalls while
 * getting there. Both are measured inside the page, against the page's own
 * clock, so Playwright's IPC never lands in a sample:
 *
 * - An interaction starts at the `click` event's `timeStamp`, captured before
 *   any app handler runs. A navigation starts at the document's time origin.
 * - Content is "visible" in the animation frame in which the target is in the
 *   DOM, laid out, and inside its scroll container's viewport; that frame is
 *   the one painted next.
 * - Stalls come from Long Animation Frames: every frame that took longer than
 *   50 ms from the start of its work to its paint, with the scripts that ran
 *   in it, so a regression points at the code that caused it.
 */
import type { Page } from "@playwright/test";

/** A frame over this long is a visible hitch (Long Animation Frames' own floor). */
export const STALL_MS = 50;
/** A frame over this long is a freeze the user notices as "stuck". */
export const FREEZE_MS = 200;

export interface FrameRecord {
  start: number;
  duration: number;
  blocking: number;
  /** The heaviest script attribution in the frame, if any. */
  source: string;
}

export interface StallSummary {
  /** Frames longer than 50 ms. */
  stalls: number;
  /** Frames longer than 200 ms. */
  freezes: number;
  stall_max_ms: number;
  /** Sum of the frames' time beyond 50 ms (Long Animation Frames' blocking duration). */
  blocking_ms: number;
  /** The scripts behind the worst frames, heaviest first. */
  top_sources: string[];
}

declare global {
  interface Window {
    __corePerf: {
      frames: FrameRecord[];
      clicks: number[];
      /** Clicks and key presses, for interactions started from the keyboard. */
      inputs: number[];
      /** Event Timing: interaction id → longest event duration (input to next paint). */
      interactions: Map<number, { start: number; duration: number }>;
      marks: Record<string, number>;
    };
  }
}

/** Installs the observers before any app script runs. Call once per context. */
export async function installFrameMetrics(page: Page, token: string): Promise<void> {
  await page.addInitScript((authToken) => {
    localStorage.setItem("multica_token", authToken);
    localStorage.setItem("multica:chat:isOpen", "false");
  }, token);
  await page.addInitScript(observeFrames);
}

/**
 * Installs the observers into a page that has already loaded, such as an
 * Electron window. Long Animation Frames are buffered, so the frames from
 * before the call are kept too.
 */
export async function attachFrameMetrics(page: Page): Promise<void> {
  await page.evaluate(observeFrames);
}

/** Runs in the page: records clicks, key presses, Long Animation Frames and Event Timing. */
function observeFrames() {
  if (window.__corePerf) return;
  const state: Window["__corePerf"] = { frames: [], clicks: [], inputs: [], interactions: new Map(), marks: {} };
  window.__corePerf = state;
  document.addEventListener("click", (event) => {
    state.clicks.push(event.timeStamp);
    state.inputs.push(event.timeStamp);
  }, { capture: true });
  document.addEventListener("keydown", (event) => state.inputs.push(event.timeStamp), { capture: true });
  new PerformanceObserver((list) => {
    for (const entry of list.getEntries() as unknown as {
      startTime: number; duration: number; blockingDuration: number;
      scripts: { duration: number; sourceURL: string; sourceFunctionName: string; invoker: string }[];
    }[]) {
      const heaviest = [...(entry.scripts ?? [])].sort((a, b) => b.duration - a.duration)[0];
      const file = heaviest?.sourceURL?.split("/").pop() ?? "";
      state.frames.push({
        start: entry.startTime,
        duration: entry.duration,
        blocking: entry.blockingDuration,
        source: heaviest
          ? `${heaviest.invoker || "script"} ${heaviest.sourceFunctionName || "(anonymous)"} @ ${file}`
          : "rendering (style/layout/paint)",
      });
    }
  }).observe({ type: "long-animation-frame", buffered: true });
  new PerformanceObserver((list) => {
    for (const entry of list.getEntries() as unknown as { interactionId: number; startTime: number; duration: number }[]) {
      if (!entry.interactionId) continue;
      // One interaction (pointerdown, pointerup, click) reports several entries.
      const prior = state.interactions.get(entry.interactionId);
      state.interactions.set(entry.interactionId, {
        start: Math.min(prior?.start ?? Infinity, entry.startTime),
        duration: Math.max(prior?.duration ?? 0, entry.duration),
      });
    }
  }).observe({ type: "event", buffered: true, durationThreshold: 16 } as PerformanceObserverInit);
}

/** The page clock, for bracketing a measured window. */
export function pageNow(page: Page): Promise<number> {
  return page.evaluate(() => performance.now());
}

/** The timestamp of the most recent click the page received. */
export async function lastClick(page: Page): Promise<number> {
  return page.evaluate(() => window.__corePerf.clicks.at(-1) ?? NaN);
}

/** The timestamp of the most recent click or key press. */
export async function lastInput(page: Page): Promise<number> {
  return page.evaluate(() => window.__corePerf.inputs.at(-1) ?? NaN);
}

/** Waits a few frames and a short idle period so trailing work lands in the window. */
export async function settle(page: Page, ms: number): Promise<void> {
  await page.waitForTimeout(ms);
  await page.evaluate(() => new Promise<void>((done) => requestAnimationFrame(() => requestAnimationFrame(() => done()))));
}

/** Long Animation Frames that started inside [from, to). */
export async function stallsBetween(page: Page, from: number, to: number): Promise<StallSummary> {
  const frames = await page.evaluate(
    ([a, b]) => window.__corePerf.frames.filter((f) => f.start >= a && f.start < b),
    [from, to] as const,
  );
  return summarizeFrames(frames);
}

export function summarizeFrames(frames: FrameRecord[]): StallSummary {
  const stalls = frames.filter((f) => f.duration > STALL_MS);
  const bySource = new Map<string, number>();
  for (const f of stalls) bySource.set(f.source, (bySource.get(f.source) ?? 0) + f.duration);
  return {
    stalls: stalls.length,
    freezes: stalls.filter((f) => f.duration > FREEZE_MS).length,
    stall_max_ms: Math.round(Math.max(0, ...stalls.map((f) => f.duration))),
    blocking_ms: Math.round(stalls.reduce((sum, f) => sum + f.blocking, 0)),
    top_sources: [...bySource.entries()]
      .sort((a, b) => b[1] - a[1])
      .slice(0, 3)
      .map(([source, ms]) => `${Math.round(ms)}ms ${source}`),
  };
}

/**
 * Longest Event Timing duration (input → next paint) of the interaction whose
 * click landed at `click`; its pointerdown came a few milliseconds earlier.
 */
export async function interactionLatency(page: Page, click: number, to: number): Promise<number | null> {
  return page.evaluate(([a, b]) => {
    let worst: number | null = null;
    for (const { start, duration } of window.__corePerf.interactions.values()) {
      if (start >= a - 100 && start < b) worst = Math.max(worst ?? 0, duration);
    }
    return worst;
  }, [click, to] as const);
}

export function quantile(values: number[], q: number): number {
  if (values.length === 0) return NaN;
  const sorted = [...values].sort((a, b) => a - b);
  const at = (sorted.length - 1) * q;
  const lo = Math.floor(at);
  const hi = Math.ceil(at);
  return sorted[lo]! + (sorted[hi]! - sorted[lo]!) * (at - lo);
}

export const median = (values: number[]) => quantile(values, 0.5);
