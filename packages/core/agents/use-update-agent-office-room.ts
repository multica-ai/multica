import { useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { useWorkspaceId } from "../hooks";
import type { Agent } from "../types";
import { workspaceKeys } from "../workspace/queries";
import type { OfficeRoom } from "./office-room";

/**
 * Moves an agent to another office room via `PUT /api/agents/:id`
 * ({ office_room }). The move is a single field with a predictable result, so
 * it patches the cached agent list at once and rolls back if the server
 * refuses. The list is invalidated on settle so the cache matches the server.
 */
export function useUpdateAgentOfficeRoom() {
  const qc = useQueryClient();
  const wsId = useWorkspaceId();

  return useMutation<Agent, Error, { agentId: string; room: OfficeRoom }, { previous?: Agent[] }>({
    mutationFn: ({ agentId, room }) => api.updateAgent(agentId, { office_room: room }),
    onMutate: async ({ agentId, room }) => {
      const queryKey = workspaceKeys.agents(wsId);
      await qc.cancelQueries({ queryKey });
      const previous = qc.getQueryData<Agent[]>(queryKey);
      qc.setQueryData<Agent[]>(queryKey, (old) =>
        old?.map((agent) => (agent.id === agentId ? { ...agent, office_room: room } : agent)),
      );
      return { previous };
    },
    onError: (_error, _vars, context) => {
      if (context?.previous) qc.setQueryData(workspaceKeys.agents(wsId), context.previous);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: workspaceKeys.agents(wsId) });
    },
  });
}
