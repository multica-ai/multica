import type { TraceStep } from "./build-steps";

/** Tool logs summarize commands only; file changes come from the run snapshot. */
export function buildRunOutcome(steps: TraceStep[]) {
  return {
    commandCount: steps.filter(
      (step) =>
        step.kind === "call" && typeof step.call?.input?.command === "string",
    ).length,
  };
}
