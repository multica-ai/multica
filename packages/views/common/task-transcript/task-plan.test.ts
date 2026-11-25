import { describe, expect, it } from "vitest";
import type { TimelineItem } from "./build-timeline";
import { buildRunPlan } from "./task-plan";

let seq = 0;
function use(tool: string, callId: string, input: Record<string, unknown>): TimelineItem {
  return { seq: ++seq, type: "tool_use", tool, callId, input };
}
// Outputs are stored as the JSON of the tool_result content, as the daemon does.
function result(callId: string, text: string): TimelineItem {
  return { seq: ++seq, type: "tool_result", callId, output: JSON.stringify(text) };
}

describe("buildRunPlan", () => {
  it("returns null when the run published no plan", () => {
    expect(buildRunPlan([use("Bash", "c1", { command: "ls" }), result("c1", "a")])).toBeNull();
  });

  it("replays Claude TaskCreate/TaskUpdate in order (shapes from Claude Code 2.1.327)", () => {
    const plan = buildRunPlan([
      use("TaskCreate", "c1", { subject: "A", description: "Task A" }),
      result("c1", "Task #1 created successfully: A"),
      use("TaskCreate", "c2", { subject: "B", description: "Task B" }),
      result("c2", "Task #2 created successfully: B"),
      use("TaskCreate", "c3", { subject: "C", description: "Task C" }),
      result("c3", "Task #3 created successfully: C"),
      use("TaskUpdate", "c4", { taskId: "1", status: "in_progress" }),
      result("c4", "Updated task #1 status"),
      use("TaskUpdate", "c5", { taskId: "1", status: "completed" }),
      result("c5", "Updated task #1 status"),
      use("TaskUpdate", "c6", { taskId: "2", status: "in_progress" }),
      result("c6", "Updated task #2 status"),
    ]);
    expect(plan).toMatchObject({ completed: 1, total: 3, current: { id: "2", text: "B" } });
    expect(plan?.steps.map((step) => step.status)).toEqual(["completed", "in_progress", "pending"]);
  });

  it("recovers a resumed session's list from TaskList and ignores unknown ids", () => {
    const plan = buildRunPlan([
      use("TaskUpdate", "c0", { taskId: "9", status: "completed" }),
      result("c0", "Updated task #9 status"),
      use("TaskList", "c1", {}),
      result("c1", "#1 [completed] A\n#2 [pending] B\n#3 [pending] C"),
      use("TaskUpdate", "c2", { taskId: "2", status: "in_progress" }),
      result("c2", "Updated task #2 status"),
    ]);
    expect(plan).toMatchObject({ completed: 1, total: 3, current: { text: "B", status: "in_progress" } });
  });

  it("drops an update with no known list and failed calls", () => {
    expect(
      buildRunPlan([
        use("TaskUpdate", "c1", { taskId: "1", status: "completed" }),
        result("c1", "Updated task #1 status"),
        use("TaskCreate", "c2", { subject: "X" }),
        result("c2", "<tool_use_error>InputValidationError</tool_use_error>"),
      ]),
    ).toBeNull();
  });

  it("handles deletion and renames", () => {
    const plan = buildRunPlan([
      use("TaskCreate", "c1", { subject: "A" }),
      result("c1", "Task #1 created successfully: A"),
      use("TaskCreate", "c2", { subject: "B" }),
      result("c2", "Task #2 created successfully: B"),
      use("TaskUpdate", "c3", { taskId: "1", status: "deleted" }),
      result("c3", "Updated task #1 deleted"),
      use("TaskUpdate", "c4", { taskId: "2", subject: "B2" }),
      result("c4", "Updated task #2 subject"),
    ]);
    expect(plan?.steps).toEqual([{ id: "2", text: "B2", status: "pending" }]);
  });

  it("takes the latest Codex update_plan snapshot", () => {
    const plan = buildRunPlan([
      use("update_plan", "p1", { plan: [{ step: "Read", status: "inProgress" }, { step: "Fix", status: "pending" }] }),
      result("p1", "Plan updated"),
      use("update_plan", "p2", {
        plan: [
          { step: "Read", status: "completed" },
          { step: "Fix", status: "inProgress" },
          { step: "Test", status: "pending" },
        ],
      }),
      result("p2", "Plan updated"),
    ]);
    expect(plan).toMatchObject({ completed: 1, total: 3, current: { text: "Fix" } });
  });

  it("does not mistake the sub-agent Task tool for the checklist", () => {
    expect(
      buildRunPlan([use("Task", "c1", { subject: "A", prompt: "go" }), result("c1", "Task #1 created successfully: A")]),
    ).toBeNull();
  });
});
