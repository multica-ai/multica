import type { TraceCallStep } from "./build-steps";
import {
  readImageResult,
  traceEventSummary,
  traceToolArgSummary,
  type TraceSummaryLabels,
} from "./trace-event-presenter";

/**
 * The one line a call is worth in the list: what it was asked to do.
 *
 * Deliberately the call's argument, never the result — the result row is where
 * base64 screenshots and 8KB command dumps used to land in the middle of the
 * list. Only a call we never saw falls back to its output.
 */
export function callSummary(step: TraceCallStep, labels: TraceSummaryLabels): string {
  if (step.call) {
    const summary = traceToolArgSummary(step.call.input, labels);
    if (summary) return summary;
  }
  if (!step.result) return "";
  if (readImageResult(step.result.output)) return "";
  return traceEventSummary({ type: "tool_result", output: step.result.output }, labels);
}

export function firstLineOf(value: string | undefined): string {
  return value?.split("\n").find((line) => line.trim().length > 0) ?? "";
}

/** Offset from the run's start, which is what a step's position means. */
export function formatOffset(ms: number): string {
  const totalSeconds = Math.max(0, Math.floor(ms / 1000));
  const minutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (minutes < 60) return `+${String(minutes).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
  const hours = Math.floor(minutes / 60);
  return `+${hours}:${String(minutes % 60).padStart(2, "0")}:${String(seconds).padStart(2, "0")}`;
}

/** A path as the run's own working directory would name it. */
export function workdirRelativePath(path: string, roots: readonly (string | undefined)[]): string {
  for (const root of roots) {
    const base = root?.trim().replace(/\/+$/, "");
    if (base && path.startsWith(`${base}/`)) return path.slice(base.length + 1);
  }
  return path;
}
