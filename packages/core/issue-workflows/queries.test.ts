// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { IssueWorkflow, IssueWorkflowStep } from "../types";
import {
  projectMoveMayHandOff,
  resolveProjectWorkflow,
  stepHandsOff,
  statusAcceptsHandoff,
  workflowAllowsStatus,
  workflowMiddleKeys,
  workflowNextStatus,
  workflowStep,
} from "./queries";

const manual = (status_key: string): IssueWorkflowStep => ({ status_key, handler: { type: "none" }, instructions: "" });

const columns = (...middle: string[]) =>
  ["backlog", "todo", ...middle, "blocked", "done", "cancelled"].map(manual);

const defaultWorkflow: IssueWorkflow = {
  id: "wf-default",
  workspace_id: "ws-1",
  name: "Default",
  description: "",
  is_default: true,
  owner_id: null,
  steps: columns("in_progress", "in_review"),
  project_ids: [],
  created_at: "",
  updated_at: "",
};

const content: IssueWorkflow = {
  ...defaultWorkflow,
  id: "wf-content",
  name: "Content",
  is_default: false,
  owner_id: null,
  steps: columns("drafting", "editing"),
};

const lead: IssueWorkflowStep = { status_key: "in_review", handler: { type: "project_lead" }, instructions: "Review it." };

describe("project workflow resolution (MUL-7420)", () => {
  it("uses the project's workflow, else Default", () => {
    expect(resolveProjectWorkflow([defaultWorkflow, content], { workflow_id: "wf-content" })?.id).toBe("wf-content");
    expect(resolveProjectWorkflow([defaultWorkflow, content], { workflow_id: null })?.id).toBe("wf-default");
    // A dangling reference renders as Default, like the server treats it.
    expect(resolveProjectWorkflow([defaultWorkflow, content], { workflow_id: "gone" })?.id).toBe("wf-default");
  });

  it("is unknown while workflows load or without a project", () => {
    expect(resolveProjectWorkflow(undefined, { workflow_id: null })).toBeNull();
    expect(resolveProjectWorkflow([defaultWorkflow], undefined)).toBeNull();
    expect(resolveProjectWorkflow([], { workflow_id: null })).toBeNull();
  });

  it("carries the workflow's own handoffs", () => {
    const reviewed = { ...content, steps: content.steps.map((s) => (s.status_key === "editing" ? { ...lead, status_key: "editing" } : s)) };
    const resolved = resolveProjectWorkflow([defaultWorkflow, reviewed], { workflow_id: "wf-content" });
    expect(stepHandsOff(workflowStep(resolved, "editing"))).toBe(true);
    expect(workflowStep(resolved, "editing")?.instructions).toBe("Review it.");
    expect(stepHandsOff(workflowStep(resolved, "drafting"))).toBe(false);
  });
});

describe("workflow helpers (MUL-7420)", () => {
  it("allows any status under Default and only listed ones under a named workflow", () => {
    expect(workflowAllowsStatus(null, "drafting")).toBe(true);
    expect(workflowAllowsStatus(defaultWorkflow, "drafting")).toBe(true);
    expect(workflowAllowsStatus(content, "drafting")).toBe(true);
    expect(workflowAllowsStatus(content, "in_progress")).toBe(false);
  });

  it("names the middle columns and who may hand off", () => {
    expect(workflowMiddleKeys(content)).toEqual(["drafting", "editing"]);
    expect(statusAcceptsHandoff("todo")).toBe(true);
    expect(statusAcceptsHandoff("blocked")).toBe(true);
    expect(statusAcceptsHandoff("backlog")).toBe(false);
    expect(statusAcceptsHandoff("done")).toBe(false);
  });

  it("points a step at the next column unless it names its own", () => {
    expect(workflowNextStatus(content, "drafting")).toBe("editing");
    // blocked is a detour, not the next step.
    expect(workflowNextStatus(content, "editing")).toBe("done");
    const own = { ...content, steps: content.steps.map((s) => (s.status_key === "drafting" ? { ...s, next_status_key: "done" } : s)) };
    expect(workflowNextStatus(own, "drafting")).toBe("done");
    expect(workflowNextStatus(content, "cancelled")).toBe("");
    expect(workflowNextStatus(content, "missing")).toBe("");
  });
});

describe("projectMoveMayHandOff", () => {
  const handsOffDrafting: IssueWorkflow = {
    ...content,
    steps: content.steps.map((s) => (s.status_key === "drafting" ? { ...s, handler: { type: "agent", id: "a-1" } } : s)),
  };

  it("checks the column the issue keeps", () => {
    expect(projectMoveMayHandOff(handsOffDrafting, "drafting")).toBe(true);
    expect(projectMoveMayHandOff(handsOffDrafting, "todo")).toBe(false);
  });

  it("asks the server when the issue lands on another column", () => {
    expect(projectMoveMayHandOff(handsOffDrafting, "in_progress")).toBe(true);
    expect(projectMoveMayHandOff(content, "in_progress")).toBe(false);
  });

  it("never asks without a workflow", () => {
    expect(projectMoveMayHandOff(null, "todo")).toBe(false);
  });
});
