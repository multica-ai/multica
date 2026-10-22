import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { IssueWorkflow, IssueWorkflowStep, Project } from "../types";

/**
 * Workspace workflows (MUL-7420).
 *
 * A project's board is its workflow's columns — the named one it picked, or
 * the workspace Default — with that workflow's handoffs on them. Helpers
 * return null only while workflows are unknown (still loading, or a server
 * without workflows), and callers then keep their pre-workflow behavior.
 */

export const issueWorkflowKeys = {
  all: (wsId: string) => ["issue-workflows", wsId] as const,
  list: (wsId: string) => [...issueWorkflowKeys.all(wsId), "list"] as const,
  handoffPreview: (wsId: string, issueId: string, status: string, projectId = "") =>
    [...issueWorkflowKeys.all(wsId), "handoff-preview", issueId, status, projectId] as const,
};

/**
 * What moving an issue to `status` — or into project `projectId` — would do.
 * Short-lived: the answer depends on the issue's assignee and runs, which
 * change under it.
 */
export function workflowHandoffPreviewOptions(wsId: string, issueId: string, status: string, projectId?: string) {
  return queryOptions({
    queryKey: issueWorkflowKeys.handoffPreview(wsId, issueId, status, projectId),
    queryFn: () => api.previewWorkflowHandoff(issueId, status, projectId),
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

/** The workspace Default workflow, once loaded. */
export function defaultWorkflow(workflows: readonly IssueWorkflow[] | undefined): IssueWorkflow | null {
  return workflows?.find((w) => w.is_default) ?? null;
}

/** Statuses with fixed places at the ends of a workflow, in the order they sit. */
export const WORKFLOW_HEAD_STATUSES = ["backlog", "todo"] as const;
export const WORKFLOW_TAIL_STATUSES = ["blocked", "done", "cancelled"] as const;
/** Fixed statuses a workflow may leave out; the others every workflow lists. */
export const WORKFLOW_OPTIONAL_STATUSES = ["backlog", "blocked"] as const;
const FIXED_STATUSES = new Set<string>([...WORKFLOW_HEAD_STATUSES, ...WORKFLOW_TAIL_STATUSES]);
const OPTIONAL_STATUSES = new Set<string>(WORKFLOW_OPTIONAL_STATUSES);
const NO_HANDOFF_STATUSES = new Set(["backlog", "done", "cancelled"]);

/** Whether a status has a fixed place when a workflow lists it. */
export function isFixedWorkflowStatus(statusKey: string): boolean {
  return FIXED_STATUSES.has(statusKey);
}

/** Whether a fixed status may be left out of a workflow. */
export function isOptionalWorkflowStatus(statusKey: string): boolean {
  return OPTIONAL_STATUSES.has(statusKey);
}

/** Whether a column can have a handler: parking and closing columns cannot. */
export function statusAcceptsHandoff(statusKey: string): boolean {
  return !NO_HANDOFF_STATUSES.has(statusKey);
}

/** The columns between the fixed head and tail, in order. */
export function workflowMiddleKeys(workflow: Pick<IssueWorkflow, "steps">): string[] {
  return workflow.steps.map((s) => s.status_key).filter((k) => !FIXED_STATUSES.has(k));
}

/**
 * The column a step's handler is told to move to when done: the step's own
 * choice, else the next column (skipping blocked); empty at the end.
 */
export function workflowNextStatus(workflow: Pick<IssueWorkflow, "steps">, statusKey: string): string {
  const index = workflow.steps.findIndex((s) => s.status_key === statusKey);
  if (index < 0) return "";
  const own = workflow.steps[index]?.next_status_key;
  if (own) return own;
  for (const s of workflow.steps.slice(index + 1)) {
    if (s.status_key !== "blocked") return s.status_key;
  }
  return "";
}

/**
 * The workflow a project uses — its named one, else Default; null while
 * workflows are unknown.
 */
export function resolveProjectWorkflow(
  workflows: readonly IssueWorkflow[] | undefined,
  project: Pick<Project, "workflow_id"> | null | undefined,
): IssueWorkflow | null {
  if (!project || !workflows) return null;
  const named = project.workflow_id ? workflows.find((w) => w.id === project.workflow_id) : undefined;
  return named ?? defaultWorkflow(workflows);
}

/**
 * The statuses every one of `issues` may take, or null when none is limited.
 * Only a named workflow limits an issue's statuses; Default and issues outside
 * a project accept the whole catalog, as the server does. A batch status
 * change offers this set, so it never shows a status the server would skip.
 */
export function sharedWorkflowStatusKeys(
  workflows: readonly IssueWorkflow[] | undefined,
  projects: readonly Pick<Project, "id" | "workflow_id">[] | undefined,
  issues: readonly { project_id: string | null }[],
): Set<string> | null {
  let shared = null as Set<string> | null;
  for (const issue of issues) {
    const project = issue.project_id ? projects?.find((p) => p.id === issue.project_id) : undefined;
    const workflow = resolveProjectWorkflow(workflows, project);
    if (!workflow || workflow.is_default) continue;
    const keys = new Set(workflow.steps.map((s) => s.status_key));
    shared = shared ? new Set([...shared].filter((key) => keys.has(key))) : keys;
  }
  return shared;
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

/**
 * Whether a status is allowed for issues of a project using `workflow`.
 * Default allows every status, as the server does, so an issue on a status
 * Default no longer lists can still be moved off and back.
 */
export function workflowAllowsStatus(
  workflow: IssueWorkflow | null | undefined,
  statusKey: string,
): boolean {
  if (!workflow || workflow.is_default) return true;
  return workflow.steps.some((s) => s.status_key === statusKey);
}

/**
 * Whether moving an issue on `statusKey` into a project using `workflow` may
 * hand it off, when the move enters the column it lands on: the column it
 * keeps hands off, or it lands on another column and some column does. The
 * server preview has the final word.
 */
export function projectMoveMayHandOff(workflow: IssueWorkflow | null | undefined, statusKey: string): boolean {
  if (!workflow) return false;
  if (workflowAllowsStatus(workflow, statusKey)) return stepHandsOff(workflowStep(workflow, statusKey));
  return workflow.steps.some(stepHandsOff);
}

/** Steps whose entry reassigns the issue. */
export function handoffStepCount(workflow: IssueWorkflow): number {
  return workflow.steps.filter((s) => s.handler.type !== "none").length;
}
