import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { governanceKeys } from "./queries";
import type { SetSquadAutonomyRequest, SquadAutonomy } from "./autonomy";

export const squadAutonomyKey = (wsId: string, squadId: string) =>
  [...governanceKeys.all(wsId), "autonomy", squadId] as const;

/** A squad's level and last reason. The squad panel and the Autonomy tab share it. */
export function squadAutonomyOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: squadAutonomyKey(wsId, squadId),
    queryFn: (): Promise<SquadAutonomy> => api.getSquadAutonomy(squadId),
    enabled: !!wsId && !!squadId,
  });
}

/**
 * Changes a squad's level. The new level is shown from the server's answer, and
 * the squads (which carry the level for the room tags) and the governance log are
 * refetched when the write settles.
 */
export function useSetSquadAutonomy(wsId: string, squadId: string) {
  const qc = useQueryClient();
  return useMutation<SquadAutonomy, Error, SetSquadAutonomyRequest>({
    mutationFn: (data) => api.setSquadAutonomy(squadId, data),
    onSuccess: (view) => {
      qc.setQueryData(squadAutonomyKey(wsId, squadId), view);
    },
    onSettled: () => {
      qc.invalidateQueries({ queryKey: ["workspaces", wsId, "squads"] });
      qc.invalidateQueries({ queryKey: governanceKeys.all(wsId) });
    },
  });
}
