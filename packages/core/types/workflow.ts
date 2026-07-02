export type PlanStatus = "draft" | "confirmed" | "running" | "done" | "cancelled";
export type WorkflowStatus = "draft" | "running" | "paused" | "done";
export type NodeStatus = "pending" | "queued" | "running" | "completed" | "failed" | "skipped";

export interface Plan {
  id: string;
  workspace_id: string;
  creator_id: string;
  title: string;
  content: string | null;
  status: PlanStatus;
  workflow_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface Workflow {
  id: string;
  plan_id: string;
  title: string;
  status: WorkflowStatus;
  created_at: string;
  updated_at: string;
}

export interface WorkflowNode {
  id: string;
  workflow_id: string;
  agent_id: string;
  title: string;
  prompt: string;
  position_x: number;
  position_y: number;
  status: NodeStatus;
  task_id: string | null;
  created_at: string;
  updated_at: string;
}

export interface WorkflowEdge {
  id: string;
  workflow_id: string;
  source_node_id: string;
  target_node_id: string;
}

export interface PlanWithWorkflow extends Plan {
  workflow: (Workflow & { nodes: WorkflowNode[]; edges: WorkflowEdge[] }) | null;
}
