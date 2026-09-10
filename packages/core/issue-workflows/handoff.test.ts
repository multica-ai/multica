// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AutomationExecution, IssueWorkflowStatusNode } from "../types";
import { workflowHandoff } from "./handoff";

const issue = { workflow_id: "flow", workflow_status_id: "dev", transition_id: "entry-2" };
const manual = { assignee: { type: "keep" }, executor: { type: "none" }, instructions: "", advance: "human_confirms" } as const;
const agent = { ...manual, executor: { type: "agent", id: "reviewer" }, instructions: "Review the change" } as const;
function node(id: string, key = id, policy = manual as IssueWorkflowStatusNode["entry_policy"]): IssueWorkflowStatusNode {
  return { id, workflow_id: "flow", spec_key: key, name: key, entry_policy: policy, phase: "started", archived_at: null } as IssueWorkflowStatusNode;
}
const dev = node("dev", "implementation", { ...manual, next_status_key: "review" });
const review = node("review", "review", agent);
function execution(overrides: Partial<AutomationExecution> = {}): AutomationExecution {
  return { id: "execution", workflow_id: "flow", status_id: "dev", trigger_transition_id: "entry-2", policy_snapshot: { ...agent, next_status_key: "review" }, status: "completed", ...overrides } as AutomationExecution;
}

describe("workflow handoff", () => {
  it("uses explicit keys across renames and reordering, never the next array position", () => {
    expect(workflowHandoff(issue, [review, dev], []).next?.id).toBe("review");
    expect(workflowHandoff(issue, [node("dev"), review], []).next).toBeUndefined();
  });
  it("keeps the entry snapshot when the project definition is edited", () => {
    const changed = { ...dev, entry_policy: { ...agent, next_status_key: "done" } };
    const result = workflowHandoff(issue, [changed, review, node("done")], [execution()]);
    expect(result.next?.id).toBe("review");
    expect(result.awaitingConfirmation).toBe(true);
  });
  it("ignores old entries and different workflow definitions", () => {
    const result = workflowHandoff(issue, [dev, { ...review, workflow_id: "other" }], [execution({ trigger_transition_id: "entry-1", status: "running" })]);
    expect(result.execution).toBeUndefined();
    expect(result.unavailableNext).toBe(true);
  });
  it("offers takeover instead of handoff while work is running", () => {
    const result = workflowHandoff(issue, [dev, review], [execution({ status: "running" })]);
    expect(result.active).toBe(true);
    expect(result.showNext).toBe(false);
  });
  it("does not add redundant actions to manual statuses or offer archived destinations", () => {
    expect(workflowHandoff(issue, [dev, node("review")], []).showNext).toBe(false);
    expect(workflowHandoff(issue, [dev, { ...review, archived_at: "2026-09-07" }], []).unavailableNext).toBe(true);
  });
  it("does not present a failed execution as an approval", () => {
    const result = workflowHandoff(issue, [dev, review], [execution({ status: "failed" })]);
    expect(result.awaitingConfirmation).toBe(false);
    expect(result.showNext).toBe(false);
  });
});
