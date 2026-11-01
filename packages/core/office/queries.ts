import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { OfficeFloor } from "./types";

export const officeKeys = {
  all: (wsId: string) => ["office", wsId] as const,
  floors: (wsId: string) => [...officeKeys.all(wsId), "floors"] as const,
  // Under the squads key tree so the broad squad invalidation that squad
  // realtime events trigger also refreshes member order.
  squadMembers: (wsId: string, squadId: string) =>
    ["workspaces", wsId, "squads", squadId, "members"] as const,
};

/** The workspace's floors in `position` order. */
export function officeFloorsOptions(wsId: string) {
  return queryOptions<OfficeFloor[]>({
    queryKey: officeKeys.floors(wsId),
    queryFn: () => api.listOfficeFloors(),
    enabled: !!wsId,
  });
}

/**
 * A squad's members in the order they were added. `layoutFloor` uses it to
 * seat agents (leader first, then join order).
 */
export function officeSquadMembersOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: officeKeys.squadMembers(wsId, squadId),
    queryFn: () => api.listSquadMembers(squadId),
    enabled: !!wsId && !!squadId,
  });
}
