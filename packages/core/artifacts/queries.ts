import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListArtifactsParams } from "../types";

export const artifactKeys = {
  all: (wsId: string) => ["artifacts", wsId] as const,
  byIssue: (wsId: string, issueId: string) =>
    [...artifactKeys.all(wsId), "by-issue", issueId] as const,
  byProject: (wsId: string, projectId: string) =>
    [...artifactKeys.all(wsId), "by-project", projectId] as const,
  detail: (wsId: string, id: string) =>
    [...artifactKeys.all(wsId), "detail", id] as const,
  search: (wsId: string, params: ListArtifactsParams) =>
    [
      ...artifactKeys.all(wsId),
      "search",
      params.kind ?? "",
      params.scope ?? "",
      params.q ?? "",
      params.limit ?? 50,
      params.offset ?? 0,
    ] as const,
  folders: (wsId: string) =>
    [...artifactKeys.all(wsId), "folders"] as const,
};

export function artifactsByIssueOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: artifactKeys.byIssue(wsId, issueId),
    queryFn: () => api.listArtifactsByIssue(issueId),
    enabled: Boolean(wsId && issueId),
  });
}

export function artifactsByProjectOptions(wsId: string, projectId: string) {
  return queryOptions({
    queryKey: artifactKeys.byProject(wsId, projectId),
    queryFn: () => api.listArtifactsByProject(projectId),
    enabled: Boolean(wsId && projectId),
  });
}

export function artifactDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: artifactKeys.detail(wsId, id),
    queryFn: () => api.getArtifact(id),
    enabled: Boolean(wsId && id),
  });
}

export function artifactSearchOptions(wsId: string, params: ListArtifactsParams) {
  return queryOptions({
    queryKey: artifactKeys.search(wsId, params),
    queryFn: () => api.searchArtifacts(params),
    enabled: Boolean(wsId),
  });
}

export function artifactFoldersOptions(wsId: string) {
  return queryOptions({
    queryKey: artifactKeys.folders(wsId),
    queryFn: () => api.listArtifactFolders(),
    enabled: Boolean(wsId),
  });
}
