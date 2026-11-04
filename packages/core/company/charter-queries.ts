import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

// Under the squads key tree, so the broad squad invalidation that squad
// realtime events trigger also refreshes the charter and routines.
export const charterKeys = {
  charter: (wsId: string, squadId: string) => ["workspaces", wsId, "squads", squadId, "charter"] as const,
  routines: (wsId: string, squadId: string) => ["workspaces", wsId, "squads", squadId, "routines"] as const,
};

/**
 * A squad's charter. A squad with none written yet reads as revision 0, so the
 * editor never needs a special case for "not written".
 */
export function squadCharterOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: charterKeys.charter(wsId, squadId),
    queryFn: () => api.getSquadCharter(squadId),
    enabled: !!wsId && !!squadId,
  });
}

/**
 * The squad's routines with their derived state, last run and next run. The
 * state is computed on the server at each read, so the list is refetched on
 * focus rather than patched by events.
 */
export function squadRoutinesOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: charterKeys.routines(wsId, squadId),
    queryFn: () => api.listSquadRoutines(squadId),
    enabled: !!wsId && !!squadId,
    staleTime: 30_000,
  });
}
