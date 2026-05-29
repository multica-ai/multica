import { queryOptions } from "@tanstack/react-query";
import {
  fetchGrantAudit,
  fetchPersonaGrant,
  fetchPersonaGrants,
  fetchSubjectsWithPermissions,
} from "./api";
import type { AuditFilter, GrantsFilter } from "./types";

export const permissionsKeys = {
  all: (wsId: string) => ["cerebro", "permissions", wsId] as const,
  grants: (wsId: string, filter: GrantsFilter) =>
    [
      ...permissionsKeys.all(wsId),
      "grants",
      filter.subjectType,
      filter.subjectId,
      filter.resourceType,
      filter.status,
      filter.classification,
      filter.limit,
      filter.offset,
    ] as const,
  grant: (wsId: string, grantId: string) =>
    [...permissionsKeys.all(wsId), "grant", grantId] as const,
  audit: (wsId: string, f: AuditFilter) =>
    [
      ...permissionsKeys.all(wsId),
      "audit",
      f.subjectId,
      f.grantId,
      f.capability,
      f.since,
      f.until,
      f.limit,
      f.offset,
    ] as const,
  subjects: (
    wsId: string,
    f: { subjectType: string | null; search: string; limit: number; offset: number },
  ) =>
    [
      ...permissionsKeys.all(wsId),
      "subjects",
      f.subjectType,
      f.search,
      f.limit,
      f.offset,
    ] as const,
};

export function grantsListOptions(wsId: string, filter: GrantsFilter) {
  return queryOptions({
    queryKey: permissionsKeys.grants(wsId, filter),
    queryFn: () => fetchPersonaGrants(wsId, filter),
    enabled: !!wsId,
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function grantDetailOptions(wsId: string, grantId: string | null) {
  return queryOptions({
    queryKey: permissionsKeys.grant(wsId, grantId ?? ""),
    queryFn: () => fetchPersonaGrant(wsId, grantId ?? ""),
    enabled: !!wsId && !!grantId,
  });
}

export function grantAuditOptions(wsId: string, filter: AuditFilter) {
  return queryOptions({
    queryKey: permissionsKeys.audit(wsId, filter),
    queryFn: () => fetchGrantAudit(wsId, filter),
    enabled: !!wsId,
    staleTime: 15 * 1000,
    placeholderData: (prev) => prev,
  });
}

export function subjectsWithPermissionsOptions(
  wsId: string,
  filter: { subjectType: string | null; search: string; limit: number; offset: number },
) {
  return queryOptions({
    queryKey: permissionsKeys.subjects(wsId, filter),
    queryFn: () => fetchSubjectsWithPermissions(wsId, filter),
    enabled: !!wsId,
    staleTime: 30 * 1000,
    placeholderData: (prev) => prev,
  });
}
