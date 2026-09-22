import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import type { CreateWorkflowRequest, Workflow, WorkflowRun, UpdateWorkflowRequest, StartWorkflowRunRequest, SendWorkflowChatMessageRequest, SubmitWorkflowWorkItemRequest, WorkflowNodeResolution } from "../types/workflow";
import { workflowKeys } from "./queries";
import { useWorkflowDraftStore, workflowDraftKey } from "./store";

function useSavedWorkflow(wsId: string) {
  const qc = useQueryClient();
  return (workflow: Workflow) => {
    qc.setQueryData(workflowKeys.detail(wsId, workflow.id), workflow);
    void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
  };
}
export function useCreateWorkflow(wsId: string) {
  const saved = useSavedWorkflow(wsId);
  return useMutation({ mutationFn: (data: CreateWorkflowRequest) => api.createWorkflow(wsId, data), onSuccess: saved });
}
export function useUpdateWorkflow(wsId: string, id: string) {
  const saved = useSavedWorkflow(wsId);
  return useMutation({ mutationFn: (data: UpdateWorkflowRequest) => api.updateWorkflow(wsId, id, data), onSuccess: saved });
}
export function useWorkflowHistory(wsId: string, id: string) {
  const saved = useSavedWorkflow(wsId);
  return useMutation({ mutationFn: ({ direction, expectedRevision, idempotencyKey }: { direction: "undo" | "redo"; expectedRevision: number; idempotencyKey?: string }) => api.changeWorkflowHistory(wsId, id, direction, expectedRevision, idempotencyKey), onSuccess: saved });
}
export function useDeleteWorkflow(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.deleteWorkflow(wsId, id),
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: workflowKeys.detail(wsId, id) });
      qc.removeQueries({ queryKey: workflowKeys.runs(wsId, id) });
      useWorkflowDraftStore.getState().discard(workflowDraftKey(wsId, id));
      void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}
function useSavedRun(wsId: string, id: string) {
  const qc = useQueryClient();
  return (run: WorkflowRun) => {
    qc.setQueryData(workflowKeys.run(wsId, id, run.id), run);
    void qc.invalidateQueries({ queryKey: workflowKeys.runs(wsId, id) });
    void qc.invalidateQueries({ queryKey: ["issues", wsId] });
  };
}
export function useStartWorkflowRun(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: (data: StartWorkflowRunRequest) => api.startWorkflowRun(wsId, id, data), onSuccess: saved });
}
export function useCancelWorkflowRun(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, expectedStateRevision, idempotencyKey }: { runId: string; expectedStateRevision?: number; idempotencyKey?: string }) => api.cancelWorkflowRun(wsId, id, runId, expectedStateRevision, idempotencyKey), onSuccess: saved });
}
export function useRetryWorkflowNode(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, nodeId, expectedStateRevision, idempotencyKey }: { runId: string; nodeId: string; expectedStateRevision?: number; idempotencyKey?: string }) => api.retryWorkflowNode(wsId, id, runId, nodeId, expectedStateRevision, idempotencyKey), onSuccess: saved });
}
export function useTerminateWorkflowRun(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, expectedStateRevision, reason, idempotencyKey }: { runId: string; expectedStateRevision?: number; reason?: string; idempotencyKey?: string }) => api.terminateWorkflowRun(wsId, id, runId, { expectedStateRevision, reason, idempotencyKey }), onSuccess: saved });
}
export function useUpdateWorkflowRun(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, data }: { runId: string; data: { expectedStateRevision: number; ownerId: string; reason?: string; idempotencyKey?: string } }) => api.updateWorkflowRun(wsId, id, runId, data), onSuccess: saved });
}
export function useTakeoverWorkflowNode(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, nodeId, data }: { runId: string; nodeId: string; data: { expectedStateRevision: number; memberId: string; reason?: string; idempotencyKey?: string } }) => api.takeoverWorkflowNode(wsId, id, runId, nodeId, data), onSuccess: saved });
}
export function useResolveWorkflowNode(wsId: string, id: string) {
  const saved = useSavedRun(wsId, id);
  return useMutation({ mutationFn: ({ runId, nodeId, data }: { runId: string; nodeId: string; data: { expectedStateRevision: number; resolution: WorkflowNodeResolution; values?: Record<string, unknown>; evidence?: string; idempotencyKey?: string } }) => api.resolveWorkflowNode(wsId, id, runId, nodeId, data), onSuccess: saved });
}
export function useSubmitWorkflowWorkItem(wsId: string, workflowId: string, runId: string, itemId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: SubmitWorkflowWorkItemRequest) => api.submitWorkflowWorkItem(wsId, workflowId, runId, itemId, data),
    onSuccess: (run) => {
      qc.setQueryData(workflowKeys.run(wsId, workflowId, runId), run);
      void qc.invalidateQueries({ queryKey: workflowKeys.runs(wsId, workflowId) });
      void qc.invalidateQueries({ queryKey: workflowKeys.workItem(wsId, workflowId, runId, itemId) });
    },
  });
}
export function useWorkflowChatSession(wsId: string, id: string) {
  return useMutation({ mutationFn: ({ agentId }: { agentId: string }) => api.workflowChatSession(wsId, id, agentId) });
}
export function useSendWorkflowChatMessage(wsId: string, id: string) {
  return useMutation({ mutationFn: (data: SendWorkflowChatMessageRequest) => api.sendWorkflowChatMessage(wsId, id, data) });
}
export function useValidateWorkflow(wsId: string, id: string) {
  return useMutation({ mutationFn: (data: { expectedRevision: number; target?: "publish" | "test" }) => api.validateWorkflow(wsId, id, data) });
}
export function usePreviewWorkflowUpgrade(wsId: string, id: string) {
  return useMutation({ mutationFn: () => api.previewWorkflowUpgrade(wsId, id) });
}
export function useUpgradeWorkflow(wsId: string, id: string) {
  const saved = useSavedWorkflow(wsId);
  return useMutation({ mutationFn: (data: { expectedRevision: number; idempotencyKey?: string }) => api.upgradeWorkflow(wsId, id, data.expectedRevision, data.idempotencyKey), onSuccess: saved });
}
export function usePublishWorkflow(wsId: string, id: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: { expectedRevision: number; notes?: string; idempotencyKey: string }) => api.publishWorkflow(wsId, id, data),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: workflowKeys.detail(wsId, id) });
      void qc.invalidateQueries({ queryKey: workflowKeys.list(wsId) });
    },
  });
}
