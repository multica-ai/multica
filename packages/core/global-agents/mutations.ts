import { useMutation, useQueryClient, type QueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";
import type {
  CreateGlobalAgentRequest,
  GlobalAgent,
  UpdateGlobalAgentRequest,
} from "../types";
import { globalAgentKeys } from "./queries";

function invalidateWorkspaceAgents(qc: QueryClient, workspaceIds: Iterable<string>) {
  for (const wsId of new Set(workspaceIds)) {
    if (wsId) qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
  }
}

function linkedWorkspaceIds(agent: Pick<GlobalAgent, "links"> | undefined) {
  return (agent?.links ?? []).map((link) => link.workspace_id);
}

export function useCreateGlobalAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (data: CreateGlobalAgentRequest) => api.createGlobalAgent(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}

/**
 * The server propagates the synced fields to every linked workspace agent, so
 * each of those workspaces' agent lists is refreshed. Links are read from the
 * response and from the cached definition in case one changed meanwhile.
 */
export function useUpdateGlobalAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...data }: { id: string } & UpdateGlobalAgentRequest) =>
      api.updateGlobalAgent(id, data),
    onSuccess: (updated, { id }) => {
      const cached = qc
        .getQueryData<GlobalAgent[]>(globalAgentKeys.list())
        ?.find((agent) => agent.id === id);
      invalidateWorkspaceAgents(qc, [
        ...linkedWorkspaceIds(cached),
        ...linkedWorkspaceIds(updated),
      ]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}

/** Linked workspace agents survive as regular agents; their lists refresh. */
export function useDeleteGlobalAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agent: Pick<GlobalAgent, "id" | "links">) =>
      api.deleteGlobalAgent(agent.id),
    onSuccess: (_data, agent) => {
      invalidateWorkspaceAgents(qc, linkedWorkspaceIds(agent));
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}

export function useEnableGlobalAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      workspace_id,
      runtime_id,
    }: {
      id: string;
      workspace_id: string;
      runtime_id: string;
    }) => api.enableGlobalAgentInWorkspace(id, { workspace_id, runtime_id }),
    onSuccess: (_agent, { workspace_id }) => {
      invalidateWorkspaceAgents(qc, [workspace_id]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}

export function useDisableGlobalAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, workspaceId }: { id: string; workspaceId: string }) =>
      api.disableGlobalAgentInWorkspace(id, workspaceId),
    onSuccess: (_agent, { workspaceId }) => {
      invalidateWorkspaceAgents(qc, [workspaceId]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}

/** Turns a workspace agent into a global agent; `wsId` is the agent's workspace. */
export function useMakeAgentGlobal(wsId: string) {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (agentId: string) => api.makeAgentGlobal(agentId),
    onSuccess: () => {
      invalidateWorkspaceAgents(qc, [wsId]);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: globalAgentKeys.all() });
    },
  });
}
