import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { companyKeys } from "./queries";
import type { OrgMap } from "./org-map-schema";

export const orgMapKeys = {
  map: (wsId: string, floorId: string | null) => [...companyKeys.all(wsId), "org-map", floorId ?? "all"] as const,
};

/** The org map of the workspace, optionally narrowed to one floor. */
export function orgMapOptions(wsId: string, floorId: string | null = null) {
  return queryOptions({
    queryKey: orgMapKeys.map(wsId, floorId),
    queryFn: (): Promise<OrgMap> => api.getCompanyOrgMap(floorId ? { floorId } : undefined),
    enabled: !!wsId,
  });
}
