import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueWorkflow, IssueWorkflowStep, Project } from "../types";

/**
 * Workspace workflows (MUL-7420).
 *
 * A project either names a workflow or uses the implicit Default workflow:
 * every active catalog status, no handoffs. Every helper here returns null
 * for Default so callers keep their pre-workflow behavior unchanged.
 */

export const issueWorkflowKeys = {
  all: (wsId: string) => ["issue-workflows", wsId] as const,
  list: (wsId: string) => [...issueWorkflowKeys.all(wsId), "list"] as const,
  handoffPreview: (wsId: string, issueId: string, status: string) =>
    [...issueWorkflowKeys.all(wsId), "handoff-preview", issueId, status] as const,
};

/**
 * What moving an issue to `status` would do. Short-lived: the answer depends
 * on the issue's assignee and runs, which change under it.
 */
export function workflowHandoffPreviewOptions(wsId: string, issueId: string, status: string) {
  return queryOptions({
    queryKey: issueWorkflowKeys.handoffPreview(wsId, issueId, status),
    queryFn: () => api.previewWorkflowHandoff(issueId, status),
    staleTime: 0,
    gcTime: 30_000,
  });
}

export function issueWorkflowListOptions(wsId: string) {
  return queryOptions({
    queryKey: issueWorkflowKeys.list(wsId),
    queryFn: () => api.listIssueWorkflows(),
    select: (data) => data.workflows,
    // Workflows change only when an admin edits one; the
    // `issue_workflow:changed` event refreshes every client when that happens.
    staleTime: 5 * 60_000,
  });
}

/** The workflow a project uses, or null for the Default workflow. */
export function resolveProjectWorkflow(
  workflows: readonly IssueWorkflow[] | undefined,
  project: Pick<Project, "workflow_id"> | null | undefined,
): IssueWorkflow | null {
  const id = project?.workflow_id;
  if (!id || !workflows) return null;
  return workflows.find((w) => w.id === id) ?? null;
}

export function workflowStep(
  workflow: IssueWorkflow | null | undefined,
  statusKey: string,
): IssueWorkflowStep | undefined {
  return workflow?.steps.find((s) => s.status_key === statusKey);
}

/** Whether entering the step reassigns the issue. */
export function stepHandsOff(step: IssueWorkflowStep | undefined): boolean {
  return !!step && step.handler.type !== "none";
}

/** Whether a status is allowed for issues of a project using `workflow`. */
export function workflowAllowsStatus(
  workflow: IssueWorkflow | null | undefined,
  statusKey: string,
): boolean {
  if (!workflow) return true;
  return workflow.steps.some((s) => s.status_key === statusKey);
}

/**
 * Where a one-click "mark done" takes an issue of a project using `workflow`:
 * `done`, or the workflow's first done-category step when it does not list
 * `done`; null when the workflow has no done step at all.
 */
export function workflowDoneStatus(
  workflow: IssueWorkflow | null | undefined,
  categoryOf: (key: string) => string,
): string | null {
  if (workflowAllowsStatus(workflow, "done")) return "done";
  return workflow?.steps.find((s) => categoryOf(s.status_key) === "done")?.status_key ?? null;
}

/**
 * Where un-marking a duplicate returns an issue: `todo`, or the workflow's
 * starting step when the workflow does not list `todo`.
 */
export function workflowReopenStatus(workflow: IssueWorkflow | null | undefined): string {
  if (!workflow || workflowAllowsStatus(workflow, "todo")) return "todo";
  return workflow.initial_status_key;
}

/** Steps whose entry reassigns the issue. */
export function handoffStepCount(workflow: IssueWorkflow): number {
  return workflow.steps.filter((s) => s.handler.type !== "none").length;
}
