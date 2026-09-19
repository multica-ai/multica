// @vitest-environment node
import { describe, expect, it } from "vitest";
import type { AutomationExecution } from "../types";
import { workflowExecutionState } from "./execution-state";

const issue = { workflow_id: "flow", workflow_status_id: "dev", transition_id: "entry-2" };
function execution(overrides: Partial<AutomationExecution> = {}): AutomationExecution {
  return { id: "execution", workflow_id: "flow", status_id: "dev", trigger_transition_id: "entry-2", status: "completed", ...overrides } as AutomationExecution;
}

describe("workflow execution state", () => {
  it("ignores old entries, different workflows and missing entry identity", () => {
    const entries = [execution({ trigger_transition_id: "entry-1" }), execution({ workflow_id: "other" }), execution({ status_id: "other" })];
    expect(workflowExecutionState(issue, entries).execution).toBeUndefined();
    expect(workflowExecutionState({ ...issue, transition_id: null }, [execution()]).execution).toBeUndefined();
  });
  it("exposes active and completed execution states independently", () => {
    for (const status of ["pending", "queued", "running", "completed", "failed", "cancelled", "superseded", "dormant"]) {
      const result = workflowExecutionState(issue, [execution({ status })]);
      expect(result.active).toBe(["pending", "queued", "running"].includes(status));
      expect(result.completed).toBe(status === "completed");
    }
  });
});
