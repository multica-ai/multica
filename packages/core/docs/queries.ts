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
  comments: (wsId: string, id: string) =>
    [...docKeys.all(wsId), "comments", id] as const,
  tasks: (wsId: string, id: string) =>
    [...docKeys.all(wsId), "tasks", id] as const,
};

/** Agent runs started from the doc's discussions, newest first. */
export function docTaskRunsOptions(wsId: string, docId: string) {
  return queryOptions({
    queryKey: docKeys.tasks(wsId, docId),
    queryFn: () => api.listDocTaskRuns(docId),
  });
}

export function docCommentsOptions(wsId: string, docId: string) {
  return queryOptions({
    queryKey: docKeys.comments(wsId, docId),
    queryFn: () => api.listDocComments(docId),
  });
}

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

export function isDocNotFound(error: unknown): boolean {
  return error instanceof ApiError && error.status === 404;
}
/** A save based on a stale revision that the server could not apply: refused
 *  outright, or refused because the stale draft and a newer edit changed the
 *  same block. Either way the draft must be reconciled with the latest doc. */
export function isDocRevisionConflict(error: unknown): boolean {
  if (!(error instanceof ApiError) || error.status !== 409) return false;
  const body = error.body as { code?: unknown } | undefined;
  return body?.code === "revision_conflict" || body?.code === "doc_merge_conflict";
}
