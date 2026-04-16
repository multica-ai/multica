import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workflowKeys } from "./queries";
import { useWorkspaceId } from "../hooks";
import type {
  Workflow,
  CreateWorkflowRequest,
  UpdateWorkflowRequest,
  ListWorkflowsResponse,
  ApproveStepRunRequest,
  SubmitPlanRequest,
  ReviewDecisionRequest,
} from "../types";

export function useCreateWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (data: CreateWorkflowRequest) => api.createWorkflow(data),
    onSuccess: (newWorkflow) => {
      qc.setQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId), (old) =>
        old && !old.workflows.some((w) => w.id === newWorkflow.id)
          ? { ...old, workflows: [...old.workflows, newWorkflow], total: old.total + 1 }
          : old,
      );
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useUpdateWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateWorkflowRequest) =>
      api.updateWorkflow(id, data),
    onMutate: ({ id, ...data }) => {
      qc.cancelQueries({ queryKey: workflowKeys.list(wsId) });
      const prevList = qc.getQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId));
      const prevDetail = qc.getQueryData<Workflow>(workflowKeys.detail(wsId, id));
      qc.setQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId), (old) =>
        old ? { ...old, workflows: old.workflows.map((w) => (w.id === id ? { ...w, ...data } : w)) } : old,
      );
      qc.setQueryData<Workflow>(workflowKeys.detail(wsId, id), (old) =>
        old ? { ...old, ...data } : old,
      );
      return { prevList, prevDetail, id };
    },
    onError: (_err, _vars, ctx) => {
      if (ctx?.prevList) qc.setQueryData(workflowKeys.list(wsId), ctx.prevList);
      if (ctx?.prevDetail) qc.setQueryData(workflowKeys.detail(wsId, ctx.id), ctx.prevDetail);
    },
    onSettled: (_data, _err, vars) => {
      qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, vars.id) });
      qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useDeleteWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkflow(id),
    onMutate: async (id) => {
      await qc.cancelQueries({ queryKey: workflowKeys.list(wsId) });
      const prevList = qc.getQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId));
      qc.setQueryData<ListWorkflowsResponse>(workflowKeys.list(wsId), (old) =>
        old ? { ...old, workflows: old.workflows.filter((w) => w.id !== id), total: old.total - 1 } : old,
      );
      qc.removeQueries({ queryKey: workflowKeys.detail(wsId, id) });
      return { prevList };
    },
    onError: (_err, _id, ctx) => {
      if (ctx?.prevList) qc.setQueryData(workflowKeys.list(wsId), ctx.prevList);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}

export function useTriggerWorkflow() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (workflowId: string) => api.triggerWorkflow(workflowId),
    onSettled: (_data, _err, workflowId) => {
      qc.invalidateQueries({ queryKey: workflowKeys.runs(wsId, workflowId) });
    },
  });
}

export function useCancelWorkflowRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: (runId: string) => api.cancelWorkflowRun(runId),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.all(wsId) });
    },
  });
}

export function useApproveStepRun() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ stepRunId, ...data }: { stepRunId: string } & ApproveStepRunRequest) =>
      api.approveStepRun(stepRunId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workflowKeys.pendingApprovals(wsId) });
    },
  });
}

export function useSubmitWorkflowPlan() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ runId, ...data }: { runId: string } & SubmitPlanRequest) =>
      api.submitWorkflowPlan(runId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.all(wsId) });
    },
  });
}

export function useSubmitReview() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();
  return useMutation({
    mutationFn: ({ stepRunId, ...data }: { stepRunId: string } & ReviewDecisionRequest) =>
      api.submitReview(stepRunId, data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workflowKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: workflowKeys.pendingApprovals(wsId) });
    },
  });
}
