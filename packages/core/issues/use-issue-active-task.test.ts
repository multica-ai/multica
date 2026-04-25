import { describe, expect, it } from "vitest";
import { isTaskPayloadForIssue } from "./use-issue-active-task";

describe("isTaskPayloadForIssue", () => {
  it("matches issue-scoped task payloads", () => {
    expect(isTaskPayloadForIssue({ issue_id: "issue-1", task_id: "task-1" }, "issue-1")).toBe(true);
    expect(isTaskPayloadForIssue({ issue_id: "issue-2", task_id: "task-1" }, "issue-1")).toBe(false);
  });

  it("ignores chat task payloads without a real issue id", () => {
    expect(isTaskPayloadForIssue({ task_id: "task-1" }, "issue-1")).toBe(false);
    expect(isTaskPayloadForIssue({ issue_id: "", task_id: "task-1" }, "issue-1")).toBe(false);
    expect(isTaskPayloadForIssue(null, "issue-1")).toBe(false);
  });
});
