import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { proposalKeys } from "./proposals";
import type { PinSquadAttentionRequest, SquadAttention } from "./squad-attention-schema";

// Under the squads key tree, like the charter, so squad realtime invalidation reaches it.
export const squadAttentionKeys = {
  attention: (wsId: string, squadId: string) => ["workspaces", wsId, "squads", squadId, "attention"] as const,
};

export function squadAttentionOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: squadAttentionKeys.attention(wsId, squadId),
    queryFn: () => api.getSquadAttention(squadId),
    enabled: !!wsId && !!squadId,
  });
}

/**
 * Pins or clears a squad's limits (owners and admins). A pin changes the caps
 * filings are checked against, so the proposal lists are refetched too.
 */
export function useSetSquadAttentionPin(wsId: string, squadId: string) {
  const qc = useQueryClient();
  const key = squadAttentionKeys.attention(wsId, squadId);
  return useMutation<SquadAttention | null, Error, PinSquadAttentionRequest>({
    mutationFn: (data) => api.setSquadAttentionPin(squadId, data),
    onSuccess: (saved) => {
      if (saved) qc.setQueryData(key, saved);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: key });
      qc.invalidateQueries({ queryKey: proposalKeys.all(wsId) });
    },
  });
}
