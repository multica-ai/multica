export type CerebroWorkflowTriggerType =
  | "status_changed"
  | "due_date_reached"
  | "due_time_reached";

export type CerebroWorkflowActionType =
  | "set_status"
  | "create_sub_issue"
  | "send_reminder"
  | "run_skill"
  | "comment_on_issue"
  | "route_by_domain";

export type CerebroWorkflowRunStatus =
  | "queued"
  | "running"
  | "success"
  | "failed"
  | "escalated";

export type CerebroWorkflowEditorMode = "form" | "canvas";

export interface CerebroWorkflow {
  id: string;
  workspace_id: string;
  project_id?: string;
  name: string;
  enabled: boolean;
  trigger_type: CerebroWorkflowTriggerType;
  trigger_config: unknown;
  conditions: unknown;
  action_type: CerebroWorkflowActionType;
  action_config: unknown;
  // Phase 2 (JEH-1103). New rows default to "form" on the server when
  // omitted, so older clients that don't send these fields keep getting
  // the form editor. editor_layout is null for form-mode rows.
  editor_mode: CerebroWorkflowEditorMode;
  editor_layout?: unknown;
  created_by_id: string;
  created_by_type: "member" | "agent";
  created_at: string;
  updated_at: string;
}

export interface CerebroWorkflowRun {
  id: string;
  workflow_id: string;
  workspace_id: string;
  target_issue_id?: string;
  task_id?: string;
  status: CerebroWorkflowRunStatus;
  attempt: number;
  error?: string;
  started_at?: string;
  finished_at?: string;
  next_retry_at?: string;
  created_at: string;
}

export interface WorkflowsListResponse {
  workflows: CerebroWorkflow[];
}

export interface WorkflowRunsListResponse {
  runs: CerebroWorkflowRun[];
  limit: number;
  offset: number;
}

// Form input shape used by the workflow-form view. Maps 1:1 onto the
// `writeWorkflowRequest` Go type on the server side.
export interface WorkflowWriteInput {
  name: string;
  enabled?: boolean;
  project_id?: string;
  trigger_type: CerebroWorkflowTriggerType;
  trigger_config?: unknown;
  conditions?: unknown;
  action_type: CerebroWorkflowActionType;
  action_config?: unknown;
  // Phase 2: optional. Omitting editor_mode keeps the server-side default of
  // "form" — form-mode workflows therefore don't need to send these fields.
  editor_mode?: CerebroWorkflowEditorMode;
  editor_layout?: unknown;
}

// Display metadata for the form builder. Order here is the order shown in
// the trigger / action dropdowns.
export const TRIGGER_OPTIONS: ReadonlyArray<{
  value: CerebroWorkflowTriggerType;
  label: string;
  description: string;
}> = [
  {
    value: "status_changed",
    label: "Status changed",
    description: "Fires when an issue moves between statuses.",
  },
  {
    value: "due_date_reached",
    label: "Due date reached",
    description: "Fires when an issue's due date arrives.",
  },
  {
    value: "due_time_reached",
    label: "Due time reached",
    description: "Fires at a specific clock time on the due date.",
  },
];

export const ACTION_OPTIONS: ReadonlyArray<{
  value: CerebroWorkflowActionType;
  label: string;
  description: string;
}> = [
  {
    value: "set_status",
    label: "Set status",
    description: "Change the issue's status to a chosen value.",
  },
  {
    value: "create_sub_issue",
    label: "Create sub-issue",
    description: "Create a child issue under the triggered issue.",
  },
  {
    value: "send_reminder",
    label: "Send reminder",
    description: "Write an inbox notification to a member or agent.",
  },
  {
    value: "run_skill",
    label: "Run skill",
    description: "Enqueue an agent task that runs a named skill with input.",
  },
  {
    value: "comment_on_issue",
    label: "Comment on issue",
    description: "Post a comment on the triggered issue or its parent.",
  },
  {
    value: "route_by_domain",
    label: "Route by domain",
    description:
      "Klassificér issuet (kode/business/design/indhold) og attach et `<prefix><domain>` label.",
  },
];

export const RUN_STATUS_BADGE: Record<CerebroWorkflowRunStatus, string> = {
  queued: "bg-muted text-muted-foreground",
  running: "bg-blue-500/10 text-blue-600 dark:text-blue-400",
  success: "bg-green-500/10 text-green-600 dark:text-green-400",
  failed: "bg-amber-500/10 text-amber-600 dark:text-amber-400",
  escalated: "bg-red-500/10 text-red-600 dark:text-red-400",
};
