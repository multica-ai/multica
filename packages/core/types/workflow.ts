export type WorkflowStatus = "draft" | "active" | "paused" | "archived";
export type WorkerType = "human" | "agent" | "squad";
export type CriticType = "human" | "agent" | "squad" | "api";
export type NodeShape = "rectangle" | "diamond" | "pill" | "hexagon";

export const NODE_SHAPES: NodeShape[] = ["rectangle", "diamond", "pill", "hexagon"];

export function parseNodeShape(formatSchema: unknown): NodeShape {
  if (
    formatSchema &&
    typeof formatSchema === "object" &&
    "shape" in (formatSchema as Record<string, unknown>) &&
    typeof (formatSchema as Record<string, unknown>).shape === "string" &&
    NODE_SHAPES.includes((formatSchema as Record<string, unknown>).shape as NodeShape)
  ) {
    return (formatSchema as Record<string, unknown>).shape as NodeShape;
  }
  return "rectangle";
}
export type NodeRunStatus =
  | "pending" | "format_checking" | "format_ok" | "format_failed"
  | "worker_assigned" | "working" | "awaiting_critic"
  | "critic_reviewing" | "critic_approved" | "critic_rework"
  | "completed" | "failed" | "blocked" | "skipped" | "cancelled";
export type WorkflowRunStatus = "running" | "completed" | "failed" | "cancelled";

export interface Workflow {
  id: string;
  workspace_id: string;
  title: string;
  description: string;
  status: WorkflowStatus;
  max_retries: number;
  created_by_type: string;
  created_by_id: string;
  node_count: number;
  is_template: boolean;
  source_template_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  title: string;
  description: string;
  position_x: number;
  position_y: number;
  format_schema: unknown;
  worker_type: WorkerType;
  worker_id: string | null;
  critic_type: CriticType;
  critic_id: string | null;
  critic_api_url: string | null;
  sort_order: number;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdge {
  id: string;
  workflow_id: string;
  source_node_id: string;
  target_node_id: string;
  condition: unknown;
  created_at: string;
}

export interface WorkflowRun {
  id: string;
  workflow_id: string;
  workspace_id: string;
  workflow_title: string;
  status: WorkflowRunStatus;
  triggered_by_type: string;
  triggered_by_id: string | null;
  input: unknown;
  output: unknown;
  started_at: string;
  completed_at: string | null;
  created_at: string;
}

export interface WorkflowNodeRun {
  id: string;
  workflow_run_id: string;
  workflow_node_id: string;
  node_title: string;
  status: NodeRunStatus;
  retry_count: number;
  worker_type: WorkerType;
  worker_id: string | null;
  worker_output: unknown;
  worker_agent_task_id: string | null;
  critic_type: CriticType;
  critic_id: string | null;
  critic_output: unknown;
  critic_comment: string;
  critic_agent_task_id: string | null;
  agent_task_id: string | null;
  started_at: string | null;
  completed_at: string | null;
  created_at: string;
  updated_at: string;
}

export interface CreateWorkflowRequest {
  title: string;
  description?: string;
  template?: string;
}

export interface UpdateWorkflowRequest {
  title?: string;
  description?: string;
  status?: WorkflowStatus;
  max_retries?: number;
}

export interface CreateNodeRequest {
  title: string;
  description?: string;
  position_x?: number;
  position_y?: number;
  format_schema?: unknown;
  worker_type: WorkerType;
  worker_id?: string | null;
  critic_type: CriticType;
  critic_id?: string | null;
  critic_api_url?: string | null;
}

export interface UpdateNodeRequest {
  title?: string;
  description?: string;
  position_x?: number;
  position_y?: number;
  format_schema?: unknown;
  worker_type?: WorkerType;
  worker_id?: string | null;
  critic_type?: CriticType;
  critic_id?: string | null;
  critic_api_url?: string | null;
  sort_order?: number;
}

export interface CreateEdgeRequest {
  source_node_id: string;
  target_node_id: string;
  condition?: unknown;
}

export interface SubmitNodeRunRequest {
  output: unknown;
}

export interface ReviewNodeRunRequest {
  approved: boolean;
  comment?: string;
}

export interface ListWorkflowsResponse {
  workflows: Workflow[];
  total: number;
}

export interface ListWorkflowRunsResponse {
  runs: WorkflowRun[];
  total: number;
}

export interface MyWorkflowTaskResponse {
  node_runs: WorkflowNodeRun[];
  total: number;
}

export interface WorkflowAdmin {
  id: string;
  name: string;
  email: string;
  can_manage_workflows: boolean;
}
