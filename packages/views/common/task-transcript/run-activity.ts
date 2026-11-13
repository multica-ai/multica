import type { TimelineItem } from "./build-timeline";
import { isCallStep, type TraceStep } from "./build-steps";

// A display threshold only; silence does not prove that a run has stalled.
export const QUIET_RUN_NOTICE_MS = 5 * 60_000;

export function runActivity(items: readonly TimelineItem[], steps: readonly TraceStep[]) {
  const latest = items.reduce<TimelineItem | undefined>(
    (last, item) => !last || item.seq > last.seq ? item : last,
    undefined,
  );
  // Older daemons and incomplete transcripts may not carry an event timestamp.
  // Never turn the run's total age into a claim about its last activity.
  const lastEventAt = latest?.created_at ? Date.parse(latest.created_at) : NaN;
  if (!Number.isFinite(lastEventAt)) return null;

  const pending = steps.findLast((step) => isCallStep(step) && step.call && !step.result);
  return {
    lastEventAt,
    pendingTool: pending && isCallStep(pending) ? pending.tool : undefined,
  };
}
