// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildRunOutcome } from "./run-outcome";
import { buildSteps, groupSteps, toolKindTotals } from "./build-steps";
import { traceEventDetail, traceToolArgSummary } from "./trace-event-presenter";
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
  it("keeps normalized Antigravity commands visible and counts each command", () => {
    const calls: TimelineItem[] = ["pwd", "git status", "go test ./..."].map((command) => ({
      seq: ++seq,
      type: "tool_use",
      tool: "run_command",
      input: {
        Cwd: "/workspace",
        command: `/bin/sh -c '${command}'`,
      },
    }));
    const steps = buildSteps(calls);

    expect(groupSteps(steps).map((row) => row.kind)).toEqual(["call", "call", "call"]);
    expect(buildRunOutcome(steps)?.commandCount).toBe(3);
    expect(calls.map((call) => traceToolArgSummary(call.input))).toEqual([
      "pwd",
      "git status",
      "go test ./...",
    ]);
    expect(traceToolArgSummary({ file_path: "/workspace/a.go" })).toBe("/workspace/a.go");
  });

  it.each([
    { name: "write", tool: "write_to_file", input: { content: "hello" }, kind: "file" },
    { name: "empty file", tool: "write_to_file", input: { content: "" }, kind: "file" },
    {
      name: "edit",
      tool: "replace_file_content",
      input: { old_string: "before", new_string: "after" },
      kind: "diff",
    },
    {
      name: "deletion",
      tool: "replace_file_content",
      input: { old_string: "before", new_string: "" },
      kind: "diff",
    },
    {
      name: "insertion",
      tool: "replace_file_content",
      input: { old_string: "", new_string: "after" },
      kind: "diff",
    },
  ])(
    "renders a normalized Antigravity $name and classifies its time as writing",
    ({ tool, input, kind }) => {
      const call: TimelineItem = {
        seq: ++seq,
        type: "tool_use",
        tool,
        input: { file_path: "/workspace/a.go", ...input },
        created_at: "2026-09-23T00:00:00.000Z",
      };
      const steps = buildSteps([
        call,
        {
          seq: ++seq,
          type: "tool_result",
          tool,
          output: "ok",
          created_at: "2026-09-23T00:00:01.000Z",
        },
      ]);

      expect(traceEventDetail(call)).toMatchObject({ kind, path: "/workspace/a.go" });
      expect(toolKindTotals(steps)).toEqual({ command: 0, write: 1000, read: 0, other: 0 });
      expect(buildRunOutcome(steps)?.paths).toEqual(["/workspace/a.go"]);
    },
  );

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
