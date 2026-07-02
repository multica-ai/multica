import type { Plan, Workflow, WorkflowNode, WorkflowEdge } from "../types/workflow";

const BASE = "/api";

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${BASE}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      ...init?.headers,
    },
  });
  if (!res.ok) {
    throw new Error(`HTTP ${res.status}: ${await res.text()}`);
  }
  if (res.status === 204) return undefined as T;
  return res.json();
}

// Plan
export const planApi = {
  list: (workspaceId: string) =>
    request<Plan[]>(`/workspaces/${workspaceId}/plans`),

  create: (workspaceId: string, body: { title: string; content?: string }) =>
    request<Plan>(`/workspaces/${workspaceId}/plans`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  get: (planId: string) =>
    request<Plan>(`/plans/${planId}`),

  update: (planId: string, body: Partial<Pick<Plan, "title" | "content" | "status">>) =>
    request<Plan>(`/plans/${planId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),
};

// Workflow
export const workflowApi = {
  get: (workflowId: string) =>
    request<Workflow>(`/workflows/${workflowId}`),

  update: (workflowId: string, body: Partial<Pick<Workflow, "title" | "status">>) =>
    request<Workflow>(`/workflows/${workflowId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  // Nodes
  listNodes: (workflowId: string) =>
    request<WorkflowNode[]>(`/workflows/${workflowId}/nodes`),

  createNode: (workflowId: string, body: {
    agent_id: string;
    title: string;
    prompt: string;
    position_x: number;
    position_y: number;
  }) =>
    request<WorkflowNode>(`/workflows/${workflowId}/nodes`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  updateNode: (workflowId: string, nodeId: string, body: Partial<{
    title: string;
    prompt: string;
    agent_id: string;
    position_x: number;
    position_y: number;
    status: string;
    task_id: string;
  }>) =>
    request<WorkflowNode>(`/workflows/${workflowId}/nodes/${nodeId}`, {
      method: "PATCH",
      body: JSON.stringify(body),
    }),

  deleteNode: (workflowId: string, nodeId: string) =>
    request<void>(`/workflows/${workflowId}/nodes/${nodeId}`, {
      method: "DELETE",
    }),

  // Edges
  listEdges: (workflowId: string) =>
    request<WorkflowEdge[]>(`/workflows/${workflowId}/edges`),

  createEdge: (workflowId: string, body: {
    source_node_id: string;
    target_node_id: string;
  }) =>
    request<WorkflowEdge>(`/workflows/${workflowId}/edges`, {
      method: "POST",
      body: JSON.stringify(body),
    }),

  deleteEdge: (workflowId: string, edgeId: string) =>
    request<void>(`/workflows/${workflowId}/edges/${edgeId}`, {
      method: "DELETE",
    }),

  confirm: (workflowId: string) =>
    request<{ workflow: Workflow; nodes: WorkflowNode[] }>(
      `/workflows/${workflowId}/confirm`,
      { method: "POST" }
    ),
};
