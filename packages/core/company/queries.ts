import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { CompanyFloor } from "./types";

export const companyKeys = {
  all: (wsId: string) => ["company", wsId] as const,
  floors: (wsId: string) => [...companyKeys.all(wsId), "floors"] as const,
  // Under the squads key tree so the broad squad invalidation that squad
  // realtime events trigger also refreshes member order.
  squadMembers: (wsId: string, squadId: string) =>
    ["workspaces", wsId, "squads", squadId, "members"] as const,
};

/** The workspace's floors in `position` order. */
export function companyFloorsOptions(wsId: string) {
  return queryOptions<CompanyFloor[]>({
    queryKey: companyKeys.floors(wsId),
    queryFn: () => api.listCompanyFloors(),
    enabled: !!wsId,
  });
}

/**
 * A squad's members in the order they were added. `layoutFloor` uses it to
 * seat agents (leader first, then join order).
 */
export function companySquadMembersOptions(wsId: string, squadId: string) {
  return queryOptions({
    queryKey: companyKeys.squadMembers(wsId, squadId),
    queryFn: () => api.listSquadMembers(squadId),
    enabled: !!wsId && !!squadId,
  });
}
