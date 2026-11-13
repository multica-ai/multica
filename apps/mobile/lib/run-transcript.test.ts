// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { TaskMessagePayload } from "@multica/core/types";
import { AgentTaskSchema } from "@/data/schemas";
import {
  buildRunTranscript,
  buildRunTranscriptEntries,
  getRunDiagnostics,
  isCancellableRunStatus,
  isRunTranscriptTaskId,
  RUN_TRANSCRIPT_PATHNAME,
  runTranscriptRoute,
  runToolContext,
} from "./run-transcript";

describe("run transcript route", () => {
  it("keeps the issue and task identifiers in the route", () => {
    expect(runTranscriptRoute("acme", "issue-7", "task-9")).toEqual({
      pathname: "/[workspace]/issue/[id]/run/[taskId]",
      params: {
        workspace: "acme",
        id: "issue-7",
        taskId: "task-9",
      },
    });
    expect(RUN_TRANSCRIPT_PATHNAME).toContain("[taskId]");
  });

  it("accepts only backend task UUIDs for transcript requests", () => {
    expect(
      isRunTranscriptTaskId("123e4567-e89b-12d3-a456-426614174000"),
    ).toBe(true);
    expect(isRunTranscriptTaskId("optimistic-task")).toBe(false);
    expect(isRunTranscriptTaskId(undefined)).toBe(false);
  });
});

function message(seq: number, type: TaskMessagePayload["type"], content?: string): TaskMessagePayload {
  return { task_id: "task-1", issue_id: "issue-1", seq, type, content };
}

describe("historical run transcript", () => {
  it("pairs parallel same-name calls by ID when results finish in reverse order", () => {
    const callA = { ...message(1, "tool_use"), tool: "Bash", call_id: "A", input: { command: "test A" } };
    const callB = { ...message(2, "tool_use"), tool: "Bash", call_id: "B", input: { command: "test B" } };
    const resultB = { ...message(3, "tool_result"), tool: "Bash", call_id: "B", output: "B failed" };
    const resultA = { ...message(4, "tool_result"), tool: "Bash", call_id: "A", output: "A passed" };
    const source = [callA, callB, resultB, resultA];
    const original = structuredClone(source);

    expect(buildRunTranscriptEntries(source)).toEqual([
      { kind: "tool", task_id: "task-1", seq: 1, call: callA, result: resultA },
      { kind: "tool", task_id: "task-1", seq: 2, call: callB, result: resultB },
    ]);
    expect(source).toEqual(original);
    expect(runToolContext(callA)).toBe("test A");
    expect(runToolContext(callB)).toBe("test B");
  });

  it("keeps unmatched and unidentified tool output without guessing a call", () => {
    const call = { ...message(1, "tool_use"), tool: "Read", call_id: "known" };
    const orphan = { ...message(2, "tool_result"), tool: "Read", call_id: "missing", output: "orphan" };
    const noId = { ...message(3, "tool_result"), tool: "Read", output: "legacy" };
    expect(buildRunTranscriptEntries([call, orphan, noId, message(4, "text", "done")])).toEqual([
      { kind: "tool", task_id: "task-1", seq: 1, call },
      { kind: "tool", task_id: "task-1", seq: 2, result: orphan },
      { kind: "tool", task_id: "task-1", seq: 3, result: noId },
      { kind: "message", task_id: "task-1", seq: 4, message: message(4, "text", "done") },
    ]);
  });

  it("does not pair identical call IDs from different tasks", () => {
    const call = { ...message(1, "tool_use"), call_id: "shared" };
    const result = { ...message(2, "tool_result"), task_id: "another-task", call_id: "shared" };
    expect(buildRunTranscriptEntries([call, result])).toHaveLength(2);
  });

  it("redacts tool context and retains file paths in the collapsed heading", () => {
    expect(runToolContext({ ...message(1, "tool_use"), input: { file_path: "src/main.ts" } })).toBe("src/main.ts");
    expect(runToolContext({ ...message(1, "tool_use"), input: { command: "echo Authorization: Bearer fake-value" } }))
      .toBe("echo Authorization: Bearer [REDACTED]");
    expect(runToolContext(undefined)).toBeUndefined();
  });

  it("sorts and joins streamed Markdown without changing the source messages", () => {
    const messages = [
      message(3, "text", "\n```"),
      message(1, "text", "```ts\n"),
      message(2, "text", "const answer = 42;"),
    ];
    const original = structuredClone(messages);

    expect(buildRunTranscript(messages)).toEqual([
      expect.objectContaining({ seq: 1, content: "```ts\nconst answer = 42;\n```" }),
    ]);
    expect(messages).toEqual(original);
  });

  it("merges adjacent thinking but keeps tool calls, results, and errors in order", () => {
    const messages = [
      message(1, "thinking", "First "),
      { ...message(2, "thinking", "check"), created_at: "2026-10-28T00:00:02Z" },
      { ...message(3, "tool_use"), tool: "Read", input: { file_path: "README.md" }, call_id: "read-1" },
      { ...message(4, "tool_result"), output: "contents", output_truncated: true, call_id: "read-1" },
      message(5, "thinking", "Then"),
      message(6, "error", "Failed"),
      message(7, "text", "Result"),
    ];

    expect(buildRunTranscript(messages)).toEqual([
      { ...messages[0], content: "First check", created_at: messages[1].created_at },
      ...messages.slice(2),
    ]);
    expect(buildRunTranscript([])).toEqual([]);
  });

  it("redacts secrets after joining fragments, including tool output", () => {
    const token = ["ghp_", "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmn"].join("");
    const result = buildRunTranscript([
      message(1, "text", token.slice(0, 12)),
      message(2, "text", token.slice(12)),
      { ...message(3, "tool_result"), output: `found ${token}` },
    ]);

    expect(result[0].content).toBe("[REDACTED GITHUB TOKEN]");
    expect(result[1].output).toBe("found [REDACTED GITHUB TOKEN]");
  });

  it("preserves startup diagnostics even when no execution messages exist", () => {
    const task = AgentTaskSchema.parse({
      id: "task-1", status: "failed", failure_reason: "environment_prepare_failed",
      error: "prepare execution environment: permission denied",
    });

    expect(buildRunTranscript([])).toEqual([]);
    expect(getRunDiagnostics(task)).toEqual({
      reasonKey: "failure_reason.environment_prepare_failed",
      error: "prepare execution environment: permission denied",
    });
  });

  it("redacts raw diagnostics and safely labels unknown failure reasons", () => {
    expect(getRunDiagnostics(AgentTaskSchema.parse({
      id: "task-1", status: "failed", failure_reason: "future_reason",
      error: "Authorization: Bearer fake-value",
    }))).toEqual({ reasonKey: "failure_reason.default", error: "Authorization: Bearer [REDACTED]" });
    expect(getRunDiagnostics(AgentTaskSchema.parse({ id: "task-1", status: "completed" }))).toBeNull();
  });
});

describe("run cancellation after schema parsing", () => {
  it.each(["queued", "deferred", "dispatched", "waiting_local_directory", "running"])(
    "keeps %s cancellable", (status) => {
      expect(isCancellableRunStatus(AgentTaskSchema.parse({ id: "task-1", status }).status)).toBe(true);
    },
  );
  it.each(["completed", "failed", "cancelled"])("does not cancel %s runs", (status) => {
    expect(isCancellableRunStatus(AgentTaskSchema.parse({ id: "task-1", status }).status)).toBe(false);
  });
});
