import type { Issue, UpdateIssueRequest } from "../types";

const MUTABLE_FIELDS = [
  "title",
  "description",
  "status",
  "priority",
  "assignee_type",
  "assignee_id",
  "position",
] as const;

export function issueWorkspaceControlWritable(issue: Issue): boolean {
  return issue.workspace_control?.writable ?? true;
}

export function updateTouchesWorkspaceControl(updates: Partial<UpdateIssueRequest>): boolean {
  return MUTABLE_FIELDS.some((field) =>
    Object.prototype.hasOwnProperty.call(updates, field),
  );
}

export function canMutateIssueThroughWorkspaceControl(
  issue: Issue,
  updates: Partial<UpdateIssueRequest>,
): boolean {
  if (!updateTouchesWorkspaceControl(updates)) return true;
  return issueWorkspaceControlWritable(issue);
}
