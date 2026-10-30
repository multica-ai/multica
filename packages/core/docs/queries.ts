import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";

/**
 * Knowledge base queries (MUL-8370). Every key lives under `docs/<wsId>`, so a
 * `doc:*`, `doc_folder:*` or `project_doc:*` realtime event refreshes the
 * whole knowledge base with one prefix invalidation.
 */
export const docKeys = {
  all: (wsId: string) => ["docs", wsId] as const,
  tree: (wsId: string) => [...docKeys.all(wsId), "tree"] as const,
  activity: (wsId: string) => [...docKeys.all(wsId), "activity"] as const,
  detail: (wsId: string, id: string) => [...docKeys.all(wsId), "detail", id] as const,
  versions: (wsId: string, id: string) => [...docKeys.all(wsId), "versions", id] as const,
  version: (wsId: string, id: string, number: number) => [...docKeys.all(wsId), "version", id, number] as const,
  project: (wsId: string, projectId: string) => [...docKeys.all(wsId), "project", projectId] as const,
};

export function docTreeOptions(wsId: string) {
  return queryOptions({
    queryKey: docKeys.tree(wsId),
    queryFn: () => api.listDocTree(),
    staleTime: 30_000,
  });
}

export function docActivityOptions(wsId: string) {
  return queryOptions({
    queryKey: docKeys.activity(wsId),
    queryFn: () => api.listDocActivity(),
    staleTime: 30_000,
  });
}

export function docDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: docKeys.detail(wsId, id),
    queryFn: () => api.getDoc(id),
  });
}

export function docVersionsOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: docKeys.versions(wsId, id),
    queryFn: () => api.listDocVersions(id),
  });
}

/**
 * One version with content. Only the newest version can still change (an
 * editing session folds into it), so older ones are cached for good.
 */
export function docVersionOptions(wsId: string, id: string, number: number, isLatest = false) {
  return queryOptions({
    queryKey: docKeys.version(wsId, id, number),
    queryFn: () => api.getDocVersion(id, number),
    staleTime: isLatest ? 0 : Infinity,
  });
}

export function projectDocsOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: docKeys.project(wsId, projectId),
    queryFn: () => api.listProjectDocs(projectId),
  });
}

/** Whether a write was refused because the doc changed since it was read. */
export function isDocRevisionConflict(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 409) return false;
  const body = error.body as { code?: unknown } | undefined;
  return body?.code === "revision_conflict";
}
