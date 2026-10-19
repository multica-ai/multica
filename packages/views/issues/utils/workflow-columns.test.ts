// @vitest-environment node
import { describe, expect, it, vi } from "vitest";
import type { IssueWorkflow } from "@multica/core/types";
import { surfaceStatusOrder, useColumnWorkflow, workflowColumns } from "./workflow-columns";

const workflow: IssueWorkflow = {
  id: "wf-1",
  workspace_id: "ws-1",
  name: "Delivery",
  description: "",
  is_default: false,
  steps: ["todo", "code_review", "in_progress", "done"].map((status_key) => ({
    status_key,
    handler: { type: "none" as const },
    instructions: "",
  })),
  project_ids: [],
  created_at: "",
  updated_at: "",
};

const { defaultWithoutBlocked } = vi.hoisted(() => ({
  defaultWithoutBlocked: {
    id: "default",
    workspace_id: "ws-1",
    name: "Default",
    description: "",
    is_default: true,
    steps: ["todo", "in_progress", "in_review", "done", "cancelled"].map((status_key) => ({
      status_key,
      handler: { type: "none" as const },
      instructions: "",
    })),
    project_ids: [],
    created_at: "",
    updated_at: "",
  },
}));

vi.mock("@multica/core/config", () => ({ useFeatureEnabled: () => true }));
vi.mock("@multica/core/issue-workflows", () => ({
  defaultWorkflow: () => defaultWithoutBlocked,
  useIssueWorkflows: () => ({ workflows: [defaultWithoutBlocked] }),
  useProjectWorkflow: () => defaultWithoutBlocked,
}));

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

describe("useColumnWorkflow (MUL-7420)", () => {
  it("follows the project's workflow on a project board", () => {
    expect(useColumnWorkflow("ws-1", "project-1", false).workflow).toBe(defaultWithoutBlocked);
  });

  it("lets an explicit status filter reach a status the project's workflow dropped", () => {
    const { workflow: selected } = useColumnWorkflow("ws-1", "project-1", true);
    expect(workflowColumns(["blocked"], selected)).toEqual(["blocked"]);
  });
});

describe("surfaceStatusOrder (MUL-7420)", () => {
  it("lists the workflow's steps, then the catalog statuses it leaves out", () => {
    expect(surfaceStatusOrder(["backlog", "todo", "in_progress", "in_review", "done", "blocked"], workflow)).toEqual([
      "todo",
      "code_review",
      "in_progress",
      "done",
      "backlog",
      "in_review",
      "blocked",
    ]);
  });

  it("is the catalog order without a workflow", () => {
    expect(surfaceStatusOrder(["backlog", "todo"], null)).toEqual(["backlog", "todo"]);
  });
});
