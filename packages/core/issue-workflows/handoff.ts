import type { AutomationExecution, Issue, IssueWorkflowStatusNode } from "../types";

export function isAutomationActive(status: string): boolean {
  return status === "pending" || status === "queued" || status === "running";
}

/** Only the execution for this entry may decide the next action. Historical
 * executions and newer project defaults must not change a pinned issue. */
export function workflowHandoff(
  issue: Pick<Issue, "workflow_id" | "workflow_status_id" | "transition_id">,
  statuses: readonly IssueWorkflowStatusNode[],
  executions: readonly AutomationExecution[],
) {
  const current = statuses.find((node) => node.id === issue.workflow_status_id && node.workflow_id === issue.workflow_id);
  const execution = executions.find((entry) =>
    entry.workflow_id === issue.workflow_id && entry.status_id === issue.workflow_status_id &&
    !!issue.transition_id && entry.trigger_transition_id === issue.transition_id,
  );
  const policy = execution?.policy_snapshot ?? current?.entry_policy;
  const next = policy?.next_status_key
    ? statuses.find((node) => node.spec_key === policy.next_status_key &&
      node.workflow_id === issue.workflow_id && node.id !== current?.id && !node.archived_at)
    : undefined;
  const active = execution !== undefined && isAutomationActive(execution.status);
  const awaitingConfirmation = execution?.status === "completed" &&
    policy?.executor.type !== "none" && policy?.advance === "human_confirms";
  const terminal = current?.phase === "completed" || current?.phase === "cancelled";
  const canHandoff = !execution || ["dormant", "completed", "superseded"].includes(execution.status);
  const showNext = !terminal && canHandoff && !active && next !== undefined && (
    awaitingConfirmation || next.entry_policy.executor.type !== "none" ||
    next.entry_policy.assignee.type !== "keep"
  );
  return { current, execution, next, active, awaitingConfirmation, showNext,
    unavailableNext: !terminal && !!policy?.next_status_key && !next };
}
