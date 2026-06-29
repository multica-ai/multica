import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const documentKeys = {
  all: (wsId: string) => ["documents", wsId] as const,
  list: (wsId: string) => [...documentKeys.all(wsId), "list"] as const,
  index: (wsId: string) => [...documentKeys.all(wsId), "index"] as const,
  detail: (wsId: string, id: string) => [...documentKeys.all(wsId), "detail", id] as const,
  revisions: (wsId: string, id: string) => [...documentKeys.all(wsId), "revisions", id] as const,
  revision: (wsId: string, id: string, rev: number) => [...documentKeys.revisions(wsId, id), rev] as const,
  issueLinks: (wsId: string, issueId: string) => [...documentKeys.all(wsId), "issue-links", issueId] as const,
};

export function documentListOptions(wsId: string) {
  return queryOptions({
    queryKey: documentKeys.list(wsId),
    queryFn: () => api.listDocuments(),
  });
}

export function documentIndexOptions(wsId: string) {
  return queryOptions({
    queryKey: documentKeys.index(wsId),
    queryFn: () => api.getDocumentIndex(),
  });
}

export function documentDetailOptions(wsId: string, docId: string) {
  return queryOptions({
    queryKey: documentKeys.detail(wsId, docId),
    queryFn: () => api.getDocument(docId),
    enabled: !!docId,
  });
}

export function documentRevisionsOptions(wsId: string, docId: string) {
  return queryOptions({
    queryKey: documentKeys.revisions(wsId, docId),
    queryFn: () => api.listDocumentRevisions(docId),
    enabled: !!docId,
  });
}

export function documentRevisionOptions(wsId: string, docId: string, revisionNumber: number) {
  return queryOptions({
    queryKey: documentKeys.revision(wsId, docId, revisionNumber),
    queryFn: () => api.getDocumentRevision(docId, revisionNumber),
    enabled: !!docId && revisionNumber > 0,
  });
}

export function issueDocumentLinksOptions(wsId: string, issueId: string) {
  return queryOptions({
    queryKey: documentKeys.issueLinks(wsId, issueId),
    queryFn: () => api.listIssueDocumentLinks(issueId),
    enabled: !!issueId,
  });
}
