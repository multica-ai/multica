// @vitest-environment node
//
// Canonical layer for the completion-result envelope's parsing matrix (CODI-11).
// The union has to absorb two backend generations at once: a v1 server sending
// `{version:1, summary, artifact_ids}` and an older one still sending
// `{output}`. Component suites must not re-run this matrix through a mount.
import { describe, expect, it } from "vitest";
import {
  AgentTaskSchema,
  AgentTaskListSchema,
  ActiveTasksForIssueSchema,
  AutopilotRunSchema,
  ListAutopilotRunsResponseSchema,
  CompletionResultSchema,
} from "./schemas";

describe("CompletionResultSchema", () => {
  it("parses a v1 envelope", () => {
    expect(
      CompletionResultSchema.parse({
        version: 1,
        summary: "the answer",
        artifact_ids: ["a1"],
      }),
    ).toEqual({ version: 1, summary: "the answer", artifact_ids: ["a1"] });
  });

  it("treats an empty summary as a real value, not a missing one", () => {
    // A tool-only turn genuinely produces no prose. Collapsing this to null
    // would make a legitimate completion look unreadable.
    expect(CompletionResultSchema.parse({ version: 1, summary: "" })).toEqual({
      version: 1,
      summary: "",
      artifact_ids: [],
    });
  });

  it("normalizes a legacy {output} payload into the v1 shape", () => {
    // An installed client pointed at a server that predates CODI-11. UI code
    // must never have to branch on which backend answered.
    expect(CompletionResultSchema.parse({ output: "legacy answer" })).toEqual({
      version: 1,
      summary: "legacy answer",
      artifact_ids: [],
    });
  });

  it("defaults missing artifact_ids to an empty array", () => {
    expect(CompletionResultSchema.parse({ version: 1, summary: "s" })?.artifact_ids).toEqual([]);
  });

  it("degrades a malformed artifact_ids without losing the summary", () => {
    // Field-level degradation: a bad array must not discard a usable answer.
    expect(
      CompletionResultSchema.parse({ version: 1, summary: "kept", artifact_ids: "nope" }),
    ).toEqual({ version: 1, summary: "kept", artifact_ids: [] });
  });

  it("degrades to null when the payload matches neither shape", () => {
    for (const bad of [{ unrelated: true }, 42, "text", []]) {
      expect(CompletionResultSchema.parse(bad)).toBeNull();
    }
  });

  it("accepts null and undefined", () => {
    expect(CompletionResultSchema.parse(null)).toBeNull();
    expect(CompletionResultSchema.parse(undefined)).toBeNull();
  });

  it("drops transport fields a legacy server may still echo", () => {
    // The regression that motivated the envelope: session_id and absolute
    // paths used to ride along inside result. The typed shape is what keeps
    // them out, structurally.
    const parsed = CompletionResultSchema.parse({
      version: 1,
      summary: "s",
      session_id: "ses_leak",
      work_dir: "/Users/alice/p",
    });
    expect(parsed).toEqual({ version: 1, summary: "s", artifact_ids: [] });
  });
});

describe("AgentTaskSchema result field", () => {
  const base = {
    id: "t1",
    agent_id: "a1",
    runtime_id: "r1",
    issue_id: "i1",
    status: "completed",
    priority: 0,
    dispatched_at: null,
    started_at: null,
    completed_at: null,
    error: null,
    created_at: "2026-01-01T00:00:00Z",
  };

  it("parses a v1 result on a task row", () => {
    const task = AgentTaskSchema.parse({
      ...base,
      result: { version: 1, summary: "done", artifact_ids: [] },
    });
    expect(task.result).toEqual({ version: 1, summary: "done", artifact_ids: [] });
  });

  it("normalizes a legacy result on a task row", () => {
    expect(AgentTaskSchema.parse({ ...base, result: { output: "old" } }).result).toEqual({
      version: 1,
      summary: "old",
      artifact_ids: [],
    });
  });

  it("keeps the task row when the result is unreadable", () => {
    // A malformed result must degrade on its own, not take the row down: the
    // status, timings and error are still worth rendering.
    const task = AgentTaskSchema.parse({ ...base, result: "garbage" });
    expect(task.result).toBeNull();
    expect(task.id).toBe("t1");
    expect(task.status).toBe("completed");
  });

  it("keeps sibling rows when one task's result is malformed", () => {
    const list = AgentTaskListSchema.parse([
      { ...base, id: "good", result: { version: 1, summary: "ok" } },
      { ...base, id: "bad", result: 12345 },
    ]);
    expect(list).toHaveLength(2);
    expect(list[1]?.result).toBeNull();
    expect(list[1]?.id).toBe("bad");
  });

  it("degrades a malformed active-task list to an empty list", () => {
    expect(ActiveTasksForIssueSchema.parse({ tasks: "nope" }).tasks).toEqual([]);
    expect(ActiveTasksForIssueSchema.parse({}).tasks).toEqual([]);
  });
});

describe("AutopilotRunSchema result field", () => {
  const base = {
    id: "run1",
    autopilot_id: "ap1",
    trigger_id: null,
    source: "manual",
    status: "completed",
    issue_id: null,
    task_id: null,
    triggered_at: "2026-01-01T00:00:00Z",
    completed_at: null,
    failure_reason: null,
    created_at: "2026-01-01T00:00:00Z",
  };

  it("parses a v1 result on a run row", () => {
    // autopilot_run.result is a verbatim copy of the task's, so it must accept
    // exactly the same shapes.
    expect(
      AutopilotRunSchema.parse({ ...base, result: { version: 1, summary: "ran" } }).result,
    ).toEqual({ version: 1, summary: "ran", artifact_ids: [] });
  });

  it("normalizes a legacy result on a run row", () => {
    expect(AutopilotRunSchema.parse({ ...base, result: { output: "old run" } }).result).toEqual({
      version: 1,
      summary: "old run",
      artifact_ids: [],
    });
  });

  it("keeps the run row when the result is unreadable", () => {
    const run = AutopilotRunSchema.parse({ ...base, result: ["bad"] });
    expect(run.result).toBeNull();
    expect(run.id).toBe("run1");
  });

  it("degrades a malformed run list without throwing", () => {
    expect(ListAutopilotRunsResponseSchema.parse({ runs: "nope", total: "x" })).toEqual({
      runs: [],
      total: 0,
    });
  });
});
