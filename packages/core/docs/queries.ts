import { queryOptions } from "@tanstack/react-query";
import { api, ApiError } from "../api";

export const docKeys = {
  all: (wsId: string) => ["docs", wsId] as const,
  tree: (wsId: string) => [...docKeys.all(wsId), "tree"] as const,
  detail: (wsId: string, id: string) =>
    [...docKeys.all(wsId), "detail", id] as const,
  versions: (wsId: string, id: string) =>
    [...docKeys.all(wsId), "versions", id] as const,
  version: (wsId: string, id: string, number: number) =>
    [...docKeys.all(wsId), "version", id, number] as const,
};

export function docTreeOptions(wsId: string) {
  return queryOptions({
    queryKey: docKeys.tree(wsId),
    queryFn: () => api.listDocTree(),
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

export function docVersionOptions(wsId: string, id: string, number: number) {
  return queryOptions({
    queryKey: docKeys.version(wsId, id, number),
    queryFn: () => api.getDocVersion(id, number),
    staleTime: Infinity,
  });
}

export function isDocRevisionConflict(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 409) return false;
  const body = error.body as { code?: unknown } | undefined;
  return body?.code === "revision_conflict";
}
