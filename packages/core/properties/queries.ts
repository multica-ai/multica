import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const propertyKeys = {
  all: (wsId: string) => ["properties", wsId] as const,
  list: (wsId: string, includeArchived = false) =>
    [...propertyKeys.all(wsId), "list", includeArchived] as const,
  /** The settings catalog with per-definition usage counts. Value writes only
   *  move counts, so they invalidate this key and leave `list` alone. */
  withUsage: (wsId: string) => [...propertyKeys.all(wsId), "with-usage"] as const,
};

/** The definition catalog. `usage_count` is always 0 here; use
 *  `propertyUsageListOptions` where counts are shown. */
export function propertyListOptions(wsId: string, includeArchived = false) {
  return queryOptions({
    queryKey: propertyKeys.list(wsId, includeArchived),
    queryFn: () => api.listProperties(includeArchived),
    select: (data) => data.properties,
  });
}

/** Every definition, archived included, with real usage counts. */
export function propertyUsageListOptions(wsId: string) {
  return queryOptions({
    queryKey: propertyKeys.withUsage(wsId),
    queryFn: () => api.listProperties(true, { includeUsage: true }),
    select: (data) => data.properties,
  });
}
