import { traceEventDetail } from "./trace-event-presenter";
import type { TraceStep } from "./build-steps";

/**
 * What a run produced, aggregated from the calls it made.
 *
 * This is the layer most readers actually want: "what did it do, can I trust
 * it" is answerable from the files it touched and the commands it ran, without
 * reading a single step. Everything here is derived from data we already
 * persist — deliberately no comment count, which would have to be guessed from
 * command text, and no pass/fail on commands, which needs an exit code the
 * stream does not carry.
 */
export interface RunOutcome {
  /** Distinct paths touched, in first-touch order. */
  paths: string[];
  addedLines: number;
  removedLines: number;
  commandCount: number;
}

function isEmpty(outcome: RunOutcome): boolean {
  return outcome.paths.length === 0 && outcome.commandCount === 0;
}

export function buildRunOutcome(steps: TraceStep[]): RunOutcome | null {
  const paths: string[] = [];
  const seen = new Set<string>();
  let addedLines = 0;
  let removedLines = 0;
  let commandCount = 0;

  const touch = (path: string) => {
    if (path.length === 0 || seen.has(path)) return;
    seen.add(path);
    paths.push(path);
  };

  for (const step of steps) {
    if (step.kind !== "call" || !step.call) continue;
    const detail = traceEventDetail(step.call);

    switch (detail.kind) {
      case "diff": {
        touch(detail.path);
        for (const line of detail.lines) {
          if (line.kind === "add") addedLines += 1;
          else if (line.kind === "remove") removedLines += 1;
        }
        break;
      }
      case "file": {
        touch(detail.path);
        addedLines += detail.lineCount;
        break;
      }
      case "patch": {
        for (const file of detail.files) {
          touch(file.path);
          if (file.body.kind === "diff") {
            for (const line of file.body.lines) {
              if (line.kind === "add") addedLines += 1;
              else if (line.kind === "remove") removedLines += 1;
            }
          } else if (file.body.kind === "file") {
            addedLines += file.body.lineCount;
          }
        }
        break;
      }
      case "text": {
        // Shell-shaped by input, not by tool name: every backend names its
        // shell differently (Bash, shell, run_command, terminal) but they all
        // pass the command in a `command` string.
        if (typeof step.call.input?.command === "string") commandCount += 1;
        break;
      }
    }
  }

  const outcome = { paths, addedLines, removedLines, commandCount };
  // A run that read files and posted a comment produced nothing this row can
  // name; showing "0 files · 0 commands" would read as "it did nothing".
  return isEmpty(outcome) ? null : outcome;
}
