import { infiniteQueryOptions, queryOptions } from "@tanstack/react-query";
import { api } from "../api";

const knowledgeStaleTime = 15_000;

export const knowledgeKeys = {
  all: (workspaceId: string) => ["knowledge", workspaceId] as const,
  bases: (workspaceId: string) => [...knowledgeKeys.all(workspaceId), "bases"] as const,
  base: (workspaceId: string, baseId: string) => [...knowledgeKeys.bases(workspaceId), baseId] as const,
  documents: (workspaceId: string, baseId: string) => [...knowledgeKeys.base(workspaceId, baseId), "documents"] as const,
  document: (workspaceId: string, baseId: string, documentId: string) => [...knowledgeKeys.documents(workspaceId, baseId), documentId] as const,
  versions: (workspaceId: string, baseId: string, documentId: string) => [...knowledgeKeys.document(workspaceId, baseId, documentId), "versions"] as const,
  blocks: (workspaceId: string, baseId: string, documentId: string, versionId: string) => [...knowledgeKeys.versions(workspaceId, baseId, documentId), "blocks", versionId] as const,
  jobs: (workspaceId: string, baseId: string) => [...knowledgeKeys.base(workspaceId, baseId), "jobs"] as const,
  entities: (workspaceId: string, baseId: string, query: string) => [...knowledgeKeys.base(workspaceId, baseId), "entities", query] as const,
  entity: (workspaceId: string, baseId: string, entityId: string) => [...knowledgeKeys.base(workspaceId, baseId), "entity", entityId] as const,
  graph: (workspaceId: string, baseId: string, entityId: string, depth: number) => [...knowledgeKeys.base(workspaceId, baseId), "graph", entityId, depth] as const,
  relationEvidence: (workspaceId: string, baseId: string, relationId: string) => [...knowledgeKeys.base(workspaceId, baseId), "relation-evidence", relationId] as const,
  graphEdits: (workspaceId: string, baseId: string) => [...knowledgeKeys.base(workspaceId, baseId), "graph-edits"] as const,
  settings: (workspaceId: string, baseId?: string) => [...knowledgeKeys.all(workspaceId), "settings", baseId ?? "workspace"] as const,
  providers: (workspaceId: string) => [...knowledgeKeys.all(workspaceId), "providers"] as const,
};

export function knowledgeBaseListOptions(workspaceId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.bases(workspaceId),
    queryFn: ({ pageParam }) => api.listKnowledgeBases(50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeBaseOptions(workspaceId: string, baseId: string) {
  return queryOptions({
    queryKey: knowledgeKeys.base(workspaceId, baseId),
    queryFn: () => api.getKnowledgeBase(baseId),
    enabled: !!workspaceId && !!baseId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeDocumentListOptions(workspaceId: string, baseId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.documents(workspaceId, baseId),
    queryFn: ({ pageParam }) => api.listKnowledgeDocuments(baseId, 50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeDocumentOptions(workspaceId: string, baseId: string, documentId: string) {
  return queryOptions({
    queryKey: knowledgeKeys.document(workspaceId, baseId, documentId),
    queryFn: () => api.getKnowledgeDocument(baseId, documentId),
    enabled: !!workspaceId && !!baseId && !!documentId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeVersionListOptions(workspaceId: string, baseId: string, documentId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.versions(workspaceId, baseId, documentId),
    queryFn: ({ pageParam }) => api.listKnowledgeVersions(baseId, documentId, 50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId && !!documentId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeBlockListOptions(workspaceId: string, baseId: string, documentId: string, versionId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.blocks(workspaceId, baseId, documentId, versionId),
    queryFn: ({ pageParam }) => api.listKnowledgeBlocks(baseId, documentId, versionId, { limit: 50, cursor: pageParam ?? undefined }),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId && !!documentId && !!versionId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeJobListOptions(workspaceId: string, baseId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.jobs(workspaceId, baseId),
    queryFn: ({ pageParam }) => api.listKnowledgeJobs(baseId, 50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId,
    staleTime: 0,
    refetchInterval: (query) => {
      const jobs = query.state.data?.pages.flatMap((page) => page.jobs) ?? [];
      return jobs.some((job) => job.status === "queued" || job.status === "running") ? 3000 : false;
    },
    refetchIntervalInBackground: false,
  });
}

export function knowledgeEntityListOptions(workspaceId: string, baseId: string, query: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.entities(workspaceId, baseId, query),
    queryFn: ({ pageParam }) => api.listKnowledgeEntities(baseId, query, undefined, 50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeEntityOptions(workspaceId: string, baseId: string, entityId: string) {
  return queryOptions({
    queryKey: knowledgeKeys.entity(workspaceId, baseId, entityId),
    queryFn: () => api.getKnowledgeEntity(baseId, entityId),
    enabled: !!workspaceId && !!baseId && !!entityId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeGraphOptions(workspaceId: string, baseId: string, entityId: string, depth: number) {
  return queryOptions({
    queryKey: knowledgeKeys.graph(workspaceId, baseId, entityId, depth),
    queryFn: () => api.getKnowledgeGraph(baseId, { entityId, depth }),
    enabled: !!workspaceId && !!baseId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeRelationEvidenceOptions(workspaceId: string, baseId: string, relationId: string) {
  return queryOptions({
    queryKey: knowledgeKeys.relationEvidence(workspaceId, baseId, relationId),
    queryFn: () => api.getKnowledgeRelationEvidence(baseId, relationId),
    enabled: !!workspaceId && !!baseId && !!relationId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeGraphEditListOptions(workspaceId: string, baseId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.graphEdits(workspaceId, baseId),
    queryFn: ({ pageParam }) => api.listKnowledgeGraphEdits(baseId, 50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId && !!baseId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeModelSettingsOptions(workspaceId: string, baseId?: string) {
  return queryOptions({
    queryKey: knowledgeKeys.settings(workspaceId, baseId),
    queryFn: () => baseId ? api.getKnowledgeBaseModelSettings(baseId) : api.getKnowledgeModelSettings(),
    enabled: !!workspaceId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeProviderListOptions(workspaceId: string) {
  return infiniteQueryOptions({
    queryKey: knowledgeKeys.providers(workspaceId),
    queryFn: ({ pageParam }) => api.listKnowledgeProviders(50, pageParam ?? undefined),
    initialPageParam: null as string | null,
    getNextPageParam: (lastPage) => lastPage.next_cursor ?? undefined,
    enabled: !!workspaceId,
    staleTime: knowledgeStaleTime,
  });
}

export function knowledgeProviderModelsOptions(workspaceId: string, providerId: string) {
  return queryOptions({
    queryKey: [...knowledgeKeys.providers(workspaceId), providerId, "models"] as const,
    queryFn: () => api.listKnowledgeProviderModels(providerId),
    enabled: !!workspaceId && !!providerId,
    staleTime: knowledgeStaleTime,
    select: (data) => data.models,
  });
}
