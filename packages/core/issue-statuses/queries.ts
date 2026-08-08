import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const issueStatusKeys = {
  /** PREFIX for invalidation — covers every catalog variant for the workspace. */
  all: (wsId: string) => ["issue-statuses", wsId] as const,
  /** FULL KEY */
  catalog: (wsId: string, includeArchived = false) =>
    [...issueStatusKeys.all(wsId), "catalog", includeArchived] as const,
};

/**
 * The workspace status catalog (MUL-4809). Server state only — never mirrored
 * into Zustand, per the state rules: the catalog is workspace data that changes
 * from the settings page and from other clients over websocket.
 *
 * `includeArchived` is admin-only server-side; leave it false for the pickers
 * and filters, which must only ever offer active statuses.
 */
export function issueStatusCatalogOptions(wsId: string, includeArchived = false) {
  return queryOptions({
    queryKey: issueStatusKeys.catalog(wsId, includeArchived),
    queryFn: () => api.listIssueStatuses(includeArchived),
  });
}

/** Just the ordered status list — the common case for pickers and filters. */
export function issueStatusListOptions(wsId: string, includeArchived = false) {
  return queryOptions({
    ...issueStatusCatalogOptions(wsId, includeArchived),
    select: (data) => data.statuses,
  });
}
