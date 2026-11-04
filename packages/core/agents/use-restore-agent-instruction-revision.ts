import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { workspaceKeys } from "../workspace/queries";
import { instructionRevisionKeys } from "./instruction-revisions-queries";

/**
 * Sets an agent's instructions back to an earlier revision. The server records
 * the restore as a new revision, so the history only grows. Waits for the server
 * and then refetches the agent and its history; nothing is patched ahead of it.
 */
export function useRestoreAgentInstructionRevision(wsId: string, agentId: string) {
  const qc = useQueryClient();
  return useMutation<void, Error, number>({
    mutationFn: (revision) => api.restoreAgentInstructionRevision(agentId, revision),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: instructionRevisionKeys.all(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agent(wsId, agentId) });
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}
