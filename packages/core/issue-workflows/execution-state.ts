import type { AutomationExecution, Issue } from "../types";

export function isAutomationActive(status: string): boolean {
  return status === "pending" || status === "queued" || status === "running";
}

/** Only the execution for this concrete status entry can be active or taken over. */
export function workflowExecutionState(
  issue: Pick<Issue, "workflow_id" | "workflow_status_id" | "transition_id">,
  executions: readonly AutomationExecution[],
) {
  const execution = executions.find((entry) =>
    entry.workflow_id === issue.workflow_id && entry.status_id === issue.workflow_status_id &&
    !!issue.transition_id && entry.trigger_transition_id === issue.transition_id,
  );
  const active = execution !== undefined && isAutomationActive(execution.status);
  const completed = execution?.status === "completed";
  return { execution, active, completed };
}
