// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueWorkflow } from "@multica/core/types";
import { workflowColumns } from "./workflow-columns";

const workflow: IssueWorkflow = {
  id: "wf-1",
  workspace_id: "ws-1",
  name: "Delivery",
  description: "",
  initial_status_key: "todo",
  steps: ["todo", "code_review", "in_progress", "done"].map((status_key) => ({
    status_key,
    handler: { type: "none" as const },
    instructions: "",
  })),
  project_ids: [],
  created_at: "",
  updated_at: "",
};

describe("workflowColumns (MUL-7420)", () => {
  it("passes catalog columns through for the Default workflow", () => {
    expect(workflowColumns(["backlog", "todo", "done"], null)).toEqual(["backlog", "todo", "done"]);
  });

  it("uses workflow order and drops statuses the workflow does not list", () => {
    expect(workflowColumns(["backlog", "todo", "in_progress", "done", "code_review"], workflow)).toEqual([
      "todo",
      "code_review",
      "in_progress",
      "done",
    ]);
  });

  it("keeps hidden columns hidden", () => {
    expect(workflowColumns(["todo", "done"], workflow)).toEqual(["todo", "done"]);
    expect(workflowColumns(null, workflow)).toEqual(["todo", "code_review", "in_progress", "done"]);
  });
});
