import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";

export interface CopyAgentParams {
  agentId: string;
  targetWorkspaceSlug: string;
  name?: string;
  targetRuntimeId?: string;
}

export function useCopyAgent() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ agentId, targetWorkspaceSlug, name, targetRuntimeId }: CopyAgentParams) =>
      api.copyAgent(agentId, {
        target_workspace_slug: targetWorkspaceSlug,
        name,
        target_runtime_id: targetRuntimeId,
      }),
    onSuccess: (agent) => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(agent.workspace_id) });
    },
  });
}
