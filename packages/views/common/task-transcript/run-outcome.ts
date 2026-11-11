import { traceEventDetail, type TraceDiffLine } from "./trace-event-presenter";
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
  /** One entry per path in `paths`, same order. */
  files: RunOutcomeFile[];
  addedLines: number;
  removedLines: number;
  commandCount: number;
}

export interface RunOutcomeFile {
  path: string;
  addedLines: number;
  removedLines: number;
  /** The first change was a whole-file write, so the run most likely created it. */
  created: boolean;
  /** The most recent step that changed this file — where its final change is. */
  lastSeq: number;
}

function isEmpty(outcome: RunOutcome): boolean {
  return outcome.paths.length === 0 && outcome.commandCount === 0;
}

function countLines(lines: readonly TraceDiffLine[]): { added: number; removed: number } {
  let added = 0;
  let removed = 0;
  for (const line of lines) {
    if (line.kind === "add") added += 1;
    else if (line.kind === "remove") removed += 1;
  }
  return { added, removed };
}

/**
 * Line counts a provider reports in its own edit result.
 *
 * Cursor's edit sends `{ path, streamContent }`, which is neither a
 * before/after pair nor a whole file, so the input alone cannot say what
 * changed. Its result does: `{"success":{"path":…,"linesAdded":N,
 * "linesRemoved":M,"diffString":…}}`. The counts come before the diff body,
 * so they survive the daemon's 8KB preview cut even when the diff does not —
 * which is why this reads them by pattern rather than parsing the JSON.
 */
export function readReportedLineCounts(
  output: string | undefined,
): { added: number; removed: number } | null {
  if (!output) return null;
  const head = output.slice(0, 2000);
  const added = /"linesAdded"\s*:\s*(\d+)/.exec(head);
  const removed = /"linesRemoved"\s*:\s*(\d+)/.exec(head);
  if (!added && !removed) return null;
  return { added: Number(added?.[1] ?? 0), removed: Number(removed?.[1] ?? 0) };
}

/** A Cursor edit: identified by input shape, like every other mutation. */
function isStreamedEdit(input: Record<string, unknown> | undefined): input is { path: string } {
  return typeof input?.path === "string" && typeof input.streamContent === "string";
}

export function buildRunOutcome(steps: TraceStep[]): RunOutcome | null {
  const files = new Map<string, RunOutcomeFile>();
  let commandCount = 0;

  const touch = (path: string, seq: number, added: number, removed: number, created: boolean) => {
    if (path.length === 0) return;
    const existing = files.get(path);
    if (existing) {
      existing.addedLines += added;
      existing.removedLines += removed;
      existing.lastSeq = seq;
      return;
    }
    files.set(path, { path, addedLines: added, removedLines: removed, created, lastSeq: seq });
  };

  for (const step of steps) {
    if (step.kind !== "call" || !step.call) continue;
    const detail = traceEventDetail(step.call);

    switch (detail.kind) {
      case "diff": {
        const { added, removed } = countLines(detail.lines);
        touch(detail.path, step.seq, added, removed, false);
        break;
      }
      case "file": {
        touch(detail.path, step.seq, detail.lineCount, 0, true);
        break;
      }
      case "patch": {
        for (const file of detail.files) {
          if (file.body.kind === "diff") {
            const { added, removed } = countLines(file.body.lines);
            touch(file.path, step.seq, added, removed, file.changeKind === "add");
          } else if (file.body.kind === "file") {
            touch(file.path, step.seq, file.body.lineCount, 0, file.changeKind !== "update");
          } else {
            touch(file.path, step.seq, 0, 0, file.changeKind === "add");
          }
        }
        break;
      }
      case "command":
      case "text": {
        const input = step.call.input;
        if (isStreamedEdit(input)) {
          const counts = readReportedLineCounts(step.result?.output) ?? { added: 0, removed: 0 };
          touch(input.path, step.seq, counts.added, counts.removed, false);
          break;
        }
        // Shell-shaped by input, not by tool name: every backend names its
        // shell differently (Bash, shell, run_command, terminal) but they all
        // pass the command in a `command` string.
        if (typeof input?.command === "string") commandCount += 1;
        break;
      }
    }
  }

  const list = Array.from(files.values());
  const outcome: RunOutcome = {
    paths: list.map((file) => file.path),
    files: list,
    addedLines: list.reduce((sum, file) => sum + file.addedLines, 0),
    removedLines: list.reduce((sum, file) => sum + file.removedLines, 0),
    commandCount,
  };
  // A run that read files and posted a comment produced nothing this row can
  // name; showing "0 files · 0 commands" would read as "it did nothing".
  return isEmpty(outcome) ? null : outcome;
}
