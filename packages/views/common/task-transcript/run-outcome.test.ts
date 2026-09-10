// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildRunOutcome } from "./run-outcome";
import { buildSteps } from "./build-steps";
import type { TimelineItem } from "./build-timeline";

let seq = 0;
function edit(path: string, before: string, after: string): TimelineItem {
  return {
    seq: ++seq,
    type: "tool_use",
    tool: "Edit",
    input: { file_path: path, old_string: before, new_string: after },
  };
}
function write(path: string, content: string): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool: "Write", input: { file_path: path, content } };
}
function bash(command: string): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool: "Bash", input: { command } };
}
function read(path: string): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool: "Read", input: { file_path: path } };
}

describe("buildRunOutcome", () => {
  it("counts changed lines from an edit", () => {
    const outcome = buildRunOutcome(buildSteps([edit("a.ts", "one\ntwo", "one\ntwo\nthree")]))!;

    expect(outcome.paths).toEqual(["a.ts"]);
    expect(outcome.addedLines).toBe(1);
    expect(outcome.removedLines).toBe(0);
  });

  it("counts a whole-file write as additions", () => {
    const outcome = buildRunOutcome(buildSteps([write("new.ts", "a\nb\nc")]))!;

    expect(outcome.paths).toEqual(["new.ts"]);
    expect(outcome.addedLines).toBe(3);
  });

  it("counts each path once across several edits", () => {
    const outcome = buildRunOutcome(
      buildSteps([edit("a.ts", "x", "y"), edit("a.ts", "y", "z"), edit("b.ts", "p", "q")]),
    )!;

    expect(outcome.paths).toEqual(["a.ts", "b.ts"]);
  });

  it("counts commands by their input shape, not the tool's name", () => {
    const custom: TimelineItem = {
      seq: ++seq,
      type: "tool_use",
      tool: "run_command",
      input: { command: "ls" },
    };
    const outcome = buildRunOutcome(buildSteps([bash("pnpm test"), custom]))!;

    expect(outcome.commandCount).toBe(2);
  });

  it("does not count a read as a change", () => {
    const outcome = buildRunOutcome(buildSteps([read("a.ts"), bash("ls")]))!;

    expect(outcome.paths).toEqual([]);
    expect(outcome.commandCount).toBe(1);
  });

  it("sums every file of a multi-file patch", () => {
    const patch: TimelineItem = {
      seq: ++seq,
      type: "tool_use",
      tool: "apply_patch",
      input: {
        changes: [
          { path: "src/a.go", kind: "update", diff: "@@ -1 +1 @@\n-old\n+new\n+extra\n" },
          { path: "src/new.go", kind: "add", content: "package main\n" },
        ],
      },
    };

    const outcome = buildRunOutcome(buildSteps([patch]))!;

    expect(outcome.paths).toEqual(["src/a.go", "src/new.go"]);
    expect(outcome.addedLines).toBe(4);
    expect(outcome.removedLines).toBe(1);
  });

  it("returns null when nothing nameable was produced", () => {
    expect(buildRunOutcome(buildSteps([read("a.ts")]))).toBeNull();
    expect(buildRunOutcome([])).toBeNull();
  });
});
