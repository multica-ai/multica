import { queryOptions, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type {
  CreateWorkflowRequest,
  UpdateWorkflowRequest,
  CreateNodeRequest,
  UpdateNodeRequest,
  CreateEdgeRequest,
  CreateStageRequest,
  UpdateStageRequest,
  ReorderStagesItem,
  AssignNodeToStageRequest,
} from "../types";

export const workflowKeys = {
  all: (wsId: string) => ["workflows", wsId] as const,
  list: (wsId: string) => [...workflowKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) => [...workflowKeys.all(wsId), "detail", id] as const,
  nodes: (wsId: string, workflowId: string) => [...workflowKeys.detail(wsId, workflowId), "nodes"] as const,
  edges: (wsId: string, workflowId: string) => [...workflowKeys.detail(wsId, workflowId), "edges"] as const,
  runs: (wsId: string, workflowId: string) => [...workflowKeys.detail(wsId, workflowId), "runs"] as const,
  run: (wsId: string, workflowId: string, runId: string) => [...workflowKeys.runs(wsId, workflowId), runId] as const,
  nodeRuns: (wsId: string, workflowId: string, runId: string) => [...workflowKeys.run(wsId, workflowId, runId), "node-runs"] as const,
  nodeRunsAll: () => ["workflows", "node-runs"] as const,
  myTasks: (wsId: string) => [...workflowKeys.all(wsId), "my-tasks"] as const,
  templates: () => ["templates"] as const,
  admins: () => ["workflow-admins"] as const,
  stages: (wsId: string, workflowId: string) => [...workflowKeys.detail(wsId, workflowId), "stages"] as const,
};

// ── Queries ──

export function workflowListOptions(wsId: string) {
  return queryOptions({
    queryKey: workflowKeys.list(wsId),
    queryFn: () => api.listWorkflows(wsId),
  });
}

export function workflowActiveListOptions(wsId: string) {
  return queryOptions({
    queryKey: [...workflowKeys.list(wsId), "active"],
    queryFn: () => api.listWorkflows(wsId),
    select: (data) => data.workflows.filter((w) => w.status === "active"),
  });
}

export function workflowDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(wsId, id),
    queryFn: () => api.getWorkflow(id),
  });
}

export function workflowNodesOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.nodes(wsId, workflowId),
    queryFn: () => api.listWorkflowNodes(workflowId),
  });
}

export function workflowEdgesOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.edges(wsId, workflowId),
    queryFn: () => api.listWorkflowEdges(workflowId),
  });
}

export function workflowRunsOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.runs(wsId, workflowId),
    queryFn: () => api.listWorkflowRuns(workflowId),
    select: (data) => data.runs,
  });
}

export function workflowRunOptions(wsId: string, workflowId: string, runId: string) {
  return queryOptions({
    queryKey: workflowKeys.run(wsId, workflowId, runId),
    queryFn: () => api.getWorkflowRun(workflowId, runId),
  });
}

export function workflowNodeRunsOptions(wsId: string, workflowId: string, runId: string) {
  return queryOptions({
    queryKey: workflowKeys.nodeRuns(wsId, workflowId, runId),
    queryFn: () => api.listWorkflowNodeRuns(workflowId, runId),
    refetchInterval: (query) => {
      const runs = query.state.data;
      if (!runs || runs.length === 0) return false;
      const terminal = new Set(["completed", "critic_approved", "failed", "blocked", "skipped", "cancelled"]);
      const allDone = runs.every((nr: { status: string }) => terminal.has(nr.status));
      return allDone ? false : 5000;
    },
  });
}

export function myWorkflowTasksOptions(wsId: string) {
  return queryOptions({
    queryKey: workflowKeys.myTasks(wsId),
    queryFn: () => api.listMyWorkflowTasks(wsId),
    select: (data) => data.node_runs,
  });
}

// ── Mutations ──

export function workflowOverviewOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.detail(wsId, workflowId),
    queryFn: () => api.getWorkflow(workflowId),
  });
}

export function workflowStagesOptions(wsId: string, workflowId: string) {
  return queryOptions({
    queryKey: workflowKeys.stages(wsId, workflowId),
    queryFn: () => api.listWorkflowStages(workflowId),
  });
}

export function useCreateWorkflow(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateWorkflowRequest) => api.createWorkflow(req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useUpdateWorkflow(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...req }: UpdateWorkflowRequest & { id: string }) =>
      api.updateWorkflow(id, req),
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, vars.id) });
    },
  });
}

export function useDeleteWorkflow(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkflow(id),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useCreateNode(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateNodeRequest) => api.createWorkflowNode(workflowId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodes(wsId, workflowId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useUpdateNode(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, ...req }: UpdateNodeRequest & { nodeId: string }) =>
      api.updateWorkflowNode(workflowId, nodeId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodes(wsId, workflowId) });
    },
  });
}

export function useDeleteNode(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (nodeId: string) => api.deleteWorkflowNode(workflowId, nodeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodes(wsId, workflowId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.edges(wsId, workflowId) });
    },
  });
}

export function useCreateEdge(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateEdgeRequest) => api.createWorkflowEdge(workflowId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.edges(wsId, workflowId) });
    },
  });
}

export function useDeleteEdge(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (edgeId: string) => api.deleteWorkflowEdge(workflowId, edgeId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.edges(wsId, workflowId) });
    },
  });
}

export function useStartWorkflowRun(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ workflowId, input }: { workflowId: string; input?: unknown }) =>
      api.startWorkflowRun(workflowId, input),
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.runs(wsId, vars.workflowId) });
    },
  });
}

export function useCancelWorkflowRun(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ workflowId, runId }: { workflowId: string; runId: string }) =>
      api.cancelWorkflowRun(workflowId, runId),
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.run(wsId, vars.workflowId, vars.runId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.runs(wsId, vars.workflowId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodeRuns(wsId, vars.workflowId, vars.runId) });
    },
  });
}

export function useSubmitNodeRun(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeRunId, output }: { nodeRunId: string; output: unknown }) =>
      api.submitNodeRun(nodeRunId, output),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.myTasks(wsId) });
    },
  });
}

export function useReviewNodeRun(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeRunId, approved, comment }: { nodeRunId: string; approved: boolean; comment?: string }) =>
      api.reviewNodeRun(nodeRunId, approved, comment),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.myTasks(wsId) });
    },
  });
}

export function useSkipNodeRun(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (nodeRunId: string) => api.skipNodeRun(nodeRunId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.myTasks(wsId) });
    },
  });
}

export function workflowTemplateListOptions(wsId: string) {
  return queryOptions({
    queryKey: workflowKeys.templates(),
    queryFn: () => api.listWorkflows(wsId, true),
  });
}

export function useCreateWorkflowFromTemplate(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ templateId, title, description }: { templateId: string; title: string; description?: string }) =>
      api.createWorkflowFromTemplate(templateId, title, description),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useToggleWorkflowTemplate(wsId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ id, isTemplate }: { id: string; isTemplate: boolean }) =>
      api.toggleWorkflowTemplate(id, isTemplate),
    onSuccess: (_data, vars) => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, vars.id) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.templates() });
    },
  });
}

export function useWorkflowAdmins() {
  return useQuery({
    queryKey: workflowKeys.admins(),
    queryFn: () => api.listWorkflowAdmins(),
  });
}

export function useUpdateWorkflowAdmins() {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (userIds: string[]) => api.updateWorkflowAdmins(userIds),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.admins() });
    },
  });
}

// ── Stage Mutations ──

export function useCreateStage(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (req: CreateStageRequest) => api.createWorkflowStage(workflowId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useUpdateStage(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ stageId, ...req }: UpdateStageRequest & { stageId: string }) =>
      api.updateWorkflowStage(workflowId, stageId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useDeleteStage(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (stageId: string) => api.deleteWorkflowStage(workflowId, stageId),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useReorderStages(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: (items: ReorderStagesItem[]) => api.reorderWorkflowStages(workflowId, items),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
    },
  });
}

export function useAssignNodeToStage(wsId: string, workflowId: string) {
  const queryClient = useQueryClient();
  return useMutation({
    mutationFn: ({ nodeId, ...req }: AssignNodeToStageRequest & { nodeId: string }) =>
      api.assignNodeToStage(workflowId, nodeId, req),
    onSuccess: () => {
      queryClient.invalidateQueries({ queryKey: workflowKeys.detail(wsId, workflowId) });
      queryClient.invalidateQueries({ queryKey: workflowKeys.nodes(wsId, workflowId) });
    },
  });
}