import type {
  IssueWorkflowResponse,
  IssueWorkflowStatusNode,
  IssueStatusCategory,
} from "../types";

export function workflowPhaseCategory(phase: string): IssueStatusCategory {
  switch (phase) {
    case "backlog":
      return "backlog";
    case "unstarted":
      return "todo";
    case "completed":
      return "done";
    case "cancelled":
      return "cancelled";
    default:
      return "in_progress";
  }
}

export function activeWorkflowStatuses(
  data?: IssueWorkflowResponse,
): IssueWorkflowStatusNode[] {
  return (data?.statuses ?? [])
    .filter(
      (node) => node.workflow_id === data?.workflow.id && !node.archived_at,
    )
    .toSorted((a, b) => a.position - b.position);
}

/** Never carry a node or a legacy key from another project into a new issue. */
export function resolveCreateWorkflowStatus(
  data: IssueWorkflowResponse | undefined,
  projectId: string | null,
  selection: { projectId: string | null; nodeId?: string; legacyKey?: string },
): IssueWorkflowStatusNode | undefined {
  const nodes = activeWorkflowStatuses(data);
  if (selection.projectId === projectId) {
    const selected = nodes.find((node) => node.id === selection.nodeId);
    if (selected) return selected;
    // Explicit legacy column defaults are supported only when unambiguous.
    if (!selection.nodeId && selection.legacyKey) {
      const matches = nodes.filter(
        (node) => node.legacy_status_key === selection.legacyKey,
      );
      if (matches.length === 1) return matches[0];
    }
  }
  return nodes.find((node) => node.id === data?.workflow.initial_status_id);
}
