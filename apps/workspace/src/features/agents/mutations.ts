import { useMutation, useQueryClient } from "@tanstack/react-query";
import type { Agent, CreateAgentRequest, SetAgentSkillsRequest, UpdateAgentRequest } from "@/shared/types";
import { api } from "@/shared/api";
import { queryKeys } from "@/shared/query";
import { useWorkspaceStore } from "@/features/workspace";

export function useAgentMutations() {
  const queryClient = useQueryClient();
  const workspaceId = useWorkspaceStore((state) => state.workspace?.id ?? null);

  const createAgentMutation = useMutation({
    mutationFn: (data: CreateAgentRequest) => api.createAgent(data),
    onSuccess: (agent) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Agent[]>(queryKeys.workspace.agents(workspaceId), (existing = []) => {
        if (existing.some((item) => item.id === agent.id)) return existing;
        return [...existing, agent];
      });
    },
  });

  const updateAgentMutation = useMutation({
    mutationFn: ({ id, data }: { id: string; data: UpdateAgentRequest }) => api.updateAgent(id, data),
    onSuccess: (agent) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Agent[]>(queryKeys.workspace.agents(workspaceId), (existing = []) =>
        existing.map((item) => (item.id === agent.id ? agent : item)),
      );
    },
  });

  const archiveAgentMutation = useMutation({
    mutationFn: (id: string) => api.archiveAgent(id),
    onSuccess: (agent) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Agent[]>(queryKeys.workspace.agents(workspaceId), (existing = []) =>
        existing.map((item) => (item.id === agent.id ? agent : item)),
      );
    },
  });

  const restoreAgentMutation = useMutation({
    mutationFn: (id: string) => api.restoreAgent(id),
    onSuccess: (agent) => {
      if (!workspaceId) return;
      queryClient.setQueryData<Agent[]>(queryKeys.workspace.agents(workspaceId), (existing = []) =>
        existing.map((item) => (item.id === agent.id ? agent : item)),
      );
    },
  });

  const setAgentSkillsMutation = useMutation({
    mutationFn: ({ agentId, data }: { agentId: string; data: SetAgentSkillsRequest }) => api.setAgentSkills(agentId, data),
    onSuccess: () => {
      if (!workspaceId) return;
      void queryClient.invalidateQueries({ queryKey: queryKeys.workspace.agents(workspaceId) });
    },
  });

  return {
    createAgent: (data: CreateAgentRequest) => createAgentMutation.mutateAsync(data),
    updateAgent: (id: string, data: UpdateAgentRequest) => updateAgentMutation.mutateAsync({ id, data }),
    archiveAgent: (id: string) => archiveAgentMutation.mutateAsync(id),
    restoreAgent: (id: string) => restoreAgentMutation.mutateAsync(id),
    setAgentSkills: (agentId: string, data: SetAgentSkillsRequest) =>
      setAgentSkillsMutation.mutateAsync({ agentId, data }),
  };
}
