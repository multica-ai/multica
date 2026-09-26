// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueWorkflow } from "../types";
import {
  resolveProjectWorkflow,
  stepHandsOff,
  workflowAllowsStatus,
  workflowDoneStatus,
  workflowReopenStatus,
  workflowStep,
} from "./queries";

const workflow: IssueWorkflow = {
  id: "wf-1",
  workspace_id: "ws-1",
  name: "Delivery",
  description: "",
  initial_status_key: "todo",
  steps: [
    { status_key: "todo", handler: { type: "none" }, instructions: "" },
    { status_key: "in_review", handler: { type: "project_lead" }, instructions: "" },
  ],
  project_ids: [],
  created_at: "",
  updated_at: "",
};

describe("workflow helpers (MUL-7420)", () => {
  it("resolves a project's workflow and treats everything else as Default", () => {
    expect(resolveProjectWorkflow([workflow], { workflow_id: "wf-1" })).toBe(workflow);
    expect(resolveProjectWorkflow([workflow], { workflow_id: null })).toBeNull();
    expect(resolveProjectWorkflow([workflow], undefined)).toBeNull();
    // A dangling reference renders as Default, like the server treats it.
    expect(resolveProjectWorkflow([workflow], { workflow_id: "gone" })).toBeNull();
    expect(resolveProjectWorkflow(undefined, { workflow_id: "wf-1" })).toBeNull();
  });

  it("allows any status under Default and only listed ones under a workflow", () => {
    expect(workflowAllowsStatus(null, "blocked")).toBe(true);
    expect(workflowAllowsStatus(workflow, "todo")).toBe(true);
    expect(workflowAllowsStatus(workflow, "blocked")).toBe(false);
  });

  it("reports which steps hand off", () => {
    expect(stepHandsOff(workflowStep(workflow, "in_review"))).toBe(true);
    expect(stepHandsOff(workflowStep(workflow, "todo"))).toBe(false);
    expect(stepHandsOff(workflowStep(workflow, "missing"))).toBe(false);
  });
});

describe("one-click status targets (MUL-7420)", () => {
  const content: IssueWorkflow = {
    ...workflow,
    initial_status_key: "topic",
    steps: [
      { status_key: "topic", handler: { type: "none" }, instructions: "" },
      { status_key: "published", handler: { type: "none" }, instructions: "" },
    ],
  };
  const categoryOf = (key: string) => (key === "published" || key === "done" ? "done" : "unstarted");

  it("marks done on the workflow's done step", () => {
    expect(workflowDoneStatus(null, categoryOf)).toBe("done");
    expect(workflowDoneStatus(content, categoryOf)).toBe("published");
    expect(workflowDoneStatus(workflow, categoryOf)).toBeNull();
  });

  it("reopens a duplicate at todo, or where the workflow starts", () => {
    expect(workflowReopenStatus(null)).toBe("todo");
    expect(workflowReopenStatus(workflow)).toBe("todo");
    expect(workflowReopenStatus(content)).toBe("topic");
  });
});
