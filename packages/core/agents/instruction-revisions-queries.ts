import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const instructionRevisionKeys = {
  all: (wsId: string, agentId: string) => ["workspaces", wsId, "agents", "detail", agentId, "instruction-revisions"] as const,
};

/** An agent's instruction history, newest first. Refetched after a save or restore. */
export function agentInstructionRevisionsOptions(wsId: string, agentId: string) {
  return queryOptions({
    queryKey: instructionRevisionKeys.all(wsId, agentId),
    queryFn: () => api.listAgentInstructionRevisions(agentId),
    enabled: !!wsId && !!agentId,
  });
}
