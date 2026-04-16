export type WorkflowStepType = "agent" | "approval" | "review" | "planner";
export type WorkflowRunStatus = "pending" | "running" | "paused" | "completed" | "failed" | "cancelled" | "planning";
export type WorkflowStepRunStatus = "pending" | "running" | "paused" | "completed" | "failed" | "cancelled";
export type ApprovalDecision = "approved" | "rejected" | "stopped";
export type ReviewDecision = "approved" | "rejected" | "stopped" | "redirect";
export type WorkflowMode = "sequential" | "orchestrated";

export interface WorkflowStep {
  type: WorkflowStepType;
  agent_id?: string;
  prompt?: string;
  reviewer_ids?: string[];
  reviewer_type?: "agent" | "member";
  review_agent_id?: string;
  review_prompt?: string;
}

export interface Workflow {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  steps: WorkflowStep[];
  mode: WorkflowMode;
  planner_agent_id: string | null;
  created_by: string;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkflowRequest {
  name: string;
  description?: string;
  steps: WorkflowStep[];
  mode?: WorkflowMode;
  planner_agent_id?: string;
}

export interface UpdateWorkflowRequest {
  name?: string;
  description?: string;
  steps?: WorkflowStep[];
  mode?: WorkflowMode;
  planner_agent_id?: string;
}

export interface WorkflowRun {
  id: string;
  workflow_id: string;
  workspace_id: string;
  trigger_type: string;
  trigger_id: string | null;
  status: WorkflowRunStatus;
  current_step_index: number;
  issue_id: string | null;
  dynamic_steps: WorkflowStep[] | null;
  planner_task_id: string | null;
  skip_review: boolean;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
}

export interface WorkflowStepRun {
  id: string;
  workflow_run_id: string;
  step_index: number;
  step_type: WorkflowStepType;
  status: WorkflowStepRunStatus;
  agent_task_id: string | null;
  reviewer_id: string | null;
  decision: ApprovalDecision | ReviewDecision | null;
  decision_comment: string | null;
  redirect_to_step: number | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
}

export interface WorkflowRunDetail extends WorkflowRun {
  step_runs: WorkflowStepRun[];
}

export interface ListWorkflowsResponse {
  workflows: Workflow[];
  total: number;
}

export interface ListWorkflowRunsResponse {
  runs: WorkflowRun[];
  total: number;
}

export interface ApproveStepRunRequest {
  decision: ApprovalDecision;
  comment?: string;
}

export interface SubmitPlanRequest {
  steps: WorkflowStep[];
}

export interface ReviewDecisionRequest {
  decision: ReviewDecision;
  comment?: string;
  redirect_to_step?: number;
}

// CEO command types
export interface CEOCommandRequest {
  message: string;
  skip_review?: boolean;
}

export interface CEOCommandResponse {
  workflow_id: string;
  workflow_run_id: string;
  issue_id?: string;
  planner_task_id?: string;
}

export interface CEOCommandItem {
  workflow_id: string;
  name: string;
  description: string;
  created_at: string;
  run_id?: string;
  run_status?: string;
}

export interface ListCEOCommandsResponse {
  commands: CEOCommandItem[];
  total: number;
}
