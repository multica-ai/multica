import type { IssueStatus, IssueWorkflow } from "@multica/core/types";

/**
 * The status columns of a surface scoped to a project that uses a workflow
 * (MUL-7420): the workflow's steps, in workflow order, narrowed to `visible`
 * (hidden-column preferences and status filters). Without a workflow the
 * catalog-ordered `visible` list passes through unchanged. `visible === null`
 * returns every step.
 */
export function workflowColumns(
  visible: readonly IssueStatus[] | null,
  workflow: IssueWorkflow | null,
): IssueStatus[] {
  if (!workflow) return visible ? [...visible] : [];
  const keys = workflow.steps.map((s) => s.status_key as IssueStatus);
  if (!visible) return keys;
  const allowed = new Set(visible);
  return keys.filter((key) => allowed.has(key));
}
