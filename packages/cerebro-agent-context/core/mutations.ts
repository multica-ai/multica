import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "@multica/core/api";
import { workspaceKeys } from "@multica/core/workspace/queries";
import { agentContextKeys } from "./queries";
import type {
  CreateAgentContextChangeRequestRequest,
  ReviewAgentContextChangeRequestRequest,
  RollbackAgentContextRequest,
} from "@multica/core/types";

export function useCreateAgentContextChangeRequest(agentId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateAgentContextChangeRequestRequest) =>
      api.createAgentContextChangeRequest(agentId, data),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: agentContextKeys.changeRequests(agentId),
      });
      qc.invalidateQueries({ queryKey: agentContextKeys.pendingAll() });
    },
  });
}

export function useReviewAgentContextChangeRequest(
  agentId: string,
  wsId: string,
) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      crId,
      data,
    }: {
      crId: string;
      data: ReviewAgentContextChangeRequestRequest;
    }) => api.reviewAgentContextChangeRequest(crId, data),
    onSuccess: () => {
      qc.invalidateQueries({
        queryKey: agentContextKeys.changeRequests(agentId),
      });
      qc.invalidateQueries({ queryKey: agentContextKeys.versions(agentId) });
      qc.invalidateQueries({ queryKey: agentContextKeys.pendingAll() });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId), exact: true });
    },
  });
}

export function useRollbackAgentContext(agentId: string, wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: RollbackAgentContextRequest) =>
      api.rollbackAgentContext(agentId, data),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: agentContextKeys.versions(agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId), exact: true });
    },
  });
}
