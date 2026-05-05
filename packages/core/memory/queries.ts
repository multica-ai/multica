import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  ListMemoryArtifactsParams,
  MemoryArtifactAnchorType,
  MemoryArtifactKind,
  SearchMemoryArtifactsParams,
} from "../types";

// Cache key factory. The wsId prefix makes workspace switching automatic
// — the cache key changes when wsId changes, so the right page renders
// without manual invalidation. Filter-bearing keys nest under `list`
// so any list invalidation hits all variants.
export const memoryKeys = {
  all: (wsId: string) => ["memory", wsId] as const,
  list: (wsId: string, params?: ListMemoryArtifactsParams) =>
    [...memoryKeys.all(wsId), "list", params ?? {}] as const,
  detail: (wsId: string, id: string) =>
    [...memoryKeys.all(wsId), "detail", id] as const,
  byAnchor: (wsId: string, anchorType: MemoryArtifactAnchorType, anchorId: string) =>
    [...memoryKeys.all(wsId), "by-anchor", anchorType, anchorId] as const,
  search: (wsId: string, params: SearchMemoryArtifactsParams) =>
    [...memoryKeys.all(wsId), "search", params] as const,
};

export function memoryListOptions(
  wsId: string,
  params?: ListMemoryArtifactsParams,
) {
  return queryOptions({
    queryKey: memoryKeys.list(wsId, params),
    queryFn: () => api.listMemoryArtifacts(params),
    select: (data) => data.memory_artifacts,
  });
}

export function memoryDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: memoryKeys.detail(wsId, id),
    queryFn: () => api.getMemoryArtifact(id),
  });
}

export function memoryByAnchorOptions(
  wsId: string,
  anchorType: MemoryArtifactAnchorType,
  anchorId: string,
  params?: { limit?: number },
) {
  return queryOptions({
    queryKey: memoryKeys.byAnchor(wsId, anchorType, anchorId),
    queryFn: () => api.listMemoryArtifactsByAnchor(anchorType, anchorId, params),
    select: (data) => data.memory_artifacts,
  });
}

// Search is a separate cache space so toggling between list and search
// views doesn't trample either's results. Empty `q` is rejected at the
// server, so don't enable this query without a non-empty term — callers
// gate enablement via `enabled: q.trim().length > 0`.
export function memorySearchOptions(
  wsId: string,
  params: SearchMemoryArtifactsParams,
) {
  return queryOptions({
    queryKey: memoryKeys.search(wsId, params),
    queryFn: () => api.searchMemoryArtifacts(params),
    select: (data) => data.memory_artifacts,
  });
}

// Convenience export — UI sometimes wants the canonical kind ordering
// for tabs/filters without re-deriving it. Keep stable; new kinds are
// appended.
export const MEMORY_KINDS: readonly MemoryArtifactKind[] = [
  "wiki_page",
  "agent_note",
  "runbook",
  "decision",
] as const;

// Display labels — short, human-friendly. Used by list filters and
// detail-page headers. Keep in sync with MEMORY_KINDS.
export const MEMORY_KIND_LABELS: Record<MemoryArtifactKind, string> = {
  wiki_page: "Wiki",
  agent_note: "Agent note",
  runbook: "Runbook",
  decision: "Decision",
};
