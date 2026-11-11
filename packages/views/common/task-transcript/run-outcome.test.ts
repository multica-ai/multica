// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildRunOutcome, readReportedLineCounts } from "./run-outcome";
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

  it("keeps per-file totals and points each file at its last change", () => {
    const first = write("src/new.ts", "a\nb\nc");
    const second = edit("src/old.ts", "x", "y");
    const third = edit("src/new.ts", "a", "a2");

    const outcome = buildRunOutcome(buildSteps([first, second, third]))!;

    expect(outcome.files).toEqual([
      { path: "src/new.ts", addedLines: 4, removedLines: 1, created: true, lastSeq: third.seq },
      { path: "src/old.ts", addedLines: 1, removedLines: 1, created: false, lastSeq: second.seq },
    ]);
  });

  it("counts a Cursor edit from the line counts its result reports", () => {
    const at = (seconds: number) => new Date(Date.parse("2026-06-08T08:00:00Z") + seconds * 1000).toISOString();
    const call: TimelineItem = {
      seq: ++seq,
      type: "tool_use",
      tool: "edit",
      input: { path: "/repo/src/app.ts", streamContent: "export const a = 2;" },
      created_at: at(0),
    };
    const result: TimelineItem = {
      seq: ++seq,
      type: "tool_result",
      tool: "edit",
      output: `{"success":{"path":"/repo/src/app.ts","linesAdded":3,"linesRemoved":1,"diffString":"${"x".repeat(9000)}`,
      created_at: at(2),
    };
    const steps = buildSteps([call, result]);
    const outcome = buildRunOutcome(steps)!;

    expect(outcome.files).toEqual([
      { path: "/repo/src/app.ts", addedLines: 3, removedLines: 1, created: false, lastSeq: call.seq },
    ]);
    expect(outcome.commandCount).toBe(0);
    expect(toolKindTotals(steps).write).toBe(2000);
  });

  it("names a Cursor edit's file even before its result arrives", () => {
    const call: TimelineItem = {
      seq: ++seq,
      type: "tool_use",
      tool: "edit",
      input: { path: "src/app.ts", streamContent: "" },
    };

    expect(buildRunOutcome(buildSteps([call]))?.files).toEqual([
      { path: "src/app.ts", addedLines: 0, removedLines: 0, created: false, lastSeq: call.seq },
    ]);
  });

  it("reads reported line counts only from a result that carries them", () => {
    expect(readReportedLineCounts('{"linesAdded": 12}')).toEqual({ added: 12, removed: 0 });
    expect(readReportedLineCounts("ok")).toBeNull();
    expect(readReportedLineCounts(undefined)).toBeNull();
  });

  it("returns null when nothing nameable was produced", () => {
    expect(buildRunOutcome(buildSteps([read("a.ts")]))).toBeNull();
    expect(buildRunOutcome([])).toBeNull();
  });
});
