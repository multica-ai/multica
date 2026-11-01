import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListKnowledgeDocumentsParams, SearchKnowledgeParams } from "./types";

export const knowledgeKeys = {
  all: (wsId: string) => ["knowledge", wsId] as const,
  bases: (wsId: string) => [...knowledgeKeys.all(wsId), "bases"] as const,
  documents: (wsId: string) => [...knowledgeKeys.all(wsId), "documents"] as const,
  /** Full key for a filtered listing; the unfiltered one shares the prefix. */
  documentList: (wsId: string, params: ListKnowledgeDocumentsParams = {}) =>
    [...knowledgeKeys.documents(wsId), params.kb ?? "", params.state ?? ""] as const,
  search: (wsId: string, params: SearchKnowledgeParams) =>
    [...knowledgeKeys.all(wsId), "search", params.kb ?? "", params.limit ?? 0, params.q] as const,
};

/** The knowledge bases a member may use, and the workspace's knowledge state. */
export function knowledgeBasesOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgeKeys.bases(wsId),
    queryFn: () => api.listKnowledgeBases(),
  });
}

export function knowledgeDocumentsOptions(wsId: string, params: ListKnowledgeDocumentsParams = {}) {
  return queryOptions({
    queryKey: knowledgeKeys.documentList(wsId, params),
    queryFn: () => api.listKnowledgeDocuments(params),
  });
}

/** Retrieval, no LLM. Disabled until there is something to search for. */
export function knowledgeSearchOptions(wsId: string, params: SearchKnowledgeParams) {
  const q = params.q.trim();
  return queryOptions({
    queryKey: knowledgeKeys.search(wsId, { ...params, q }),
    queryFn: () => api.searchKnowledge({ ...params, q }),
    enabled: q.length > 0,
  });
}
