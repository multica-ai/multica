// @vitest-environment node
import { describe, expect, it } from "vitest";
import { buildRunOutcome } from "./run-outcome";
import { buildSteps, groupSteps, toolKindTotals } from "./build-steps";
import { traceEventDetail, traceToolArgSummary } from "./trace-event-presenter";
import type { TimelineItem } from "./build-timeline";

let seq = 0;
function bash(command: string): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool: "Bash", input: { command } };
}
function read(path: string): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool: "Read", input: { file_path: path } };
}

describe("buildRunOutcome", () => {
  // The outcome summarizes commands only — file
  // changes come from the run snapshot, never derived from tool calls.
  it("summarizes shell calls without deriving file changes from tools", () => {
    const steps = buildSteps([
      { type: "tool_use", seq: 1, tool: "shell", input: { command: "echo ok" } },
      {
        type: "tool_use",
        seq: 2,
        tool: "edit",
        input: { file_path: "a", old_string: "old", new_string: "new" },
      },
    ]);
    expect(buildRunOutcome(steps)).toEqual({ commandCount: 1 });
  });

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
    },
  );

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

  it("does not count a read as a command", () => {
    const outcome = buildRunOutcome(buildSteps([read("a.ts"), bash("ls")]))!;

    expect(outcome.commandCount).toBe(1);
  });
});
