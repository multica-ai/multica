import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type {
  ListKnowledgeAnalyticsParams,
  ListKnowledgeCandidatesParams,
  ListKnowledgeEffectParams,
  ListKnowledgeGovernanceFindingsParams,
  ListKnowledgeParams,
} from "./types";

export const knowledgeKeys = {
  all: (wsId: string) => ["knowledge", wsId] as const,
  list: (wsId: string, params?: ListKnowledgeParams) =>
    [...knowledgeKeys.all(wsId), "list", params ?? {}] as const,
  detail: (wsId: string, id: string) =>
    [...knowledgeKeys.all(wsId), "detail", id] as const,
  sources: (wsId: string, id: string) =>
    [...knowledgeKeys.all(wsId), "sources", id] as const,
  candidates: (wsId: string, params?: ListKnowledgeCandidatesParams) =>
    [...knowledgeKeys.all(wsId), "candidates", params ?? {}] as const,
  governanceFindings: (wsId: string, params?: ListKnowledgeGovernanceFindingsParams) =>
    [...knowledgeKeys.all(wsId), "governance-findings", params ?? {}] as const,
  analytics: (wsId: string, params?: ListKnowledgeAnalyticsParams) =>
    [...knowledgeKeys.all(wsId), "analytics", params ?? {}] as const,
  effect: (wsId: string, params?: ListKnowledgeEffectParams) =>
    [...knowledgeKeys.all(wsId), "effect", params ?? {}] as const,
  effectSummary: (wsId: string, params?: Omit<ListKnowledgeEffectParams, "limit" | "offset">) =>
    [...knowledgeKeys.all(wsId), "effect-summary", params ?? {}] as const,
};

export function knowledgeListOptions(wsId: string, params?: ListKnowledgeParams) {
  return queryOptions({
    queryKey: knowledgeKeys.list(wsId, params),
    queryFn: () => api.listKnowledge(params),
  });
}

export function knowledgeDetailOptions(wsId: string, id: string | null) {
  return queryOptions({
    queryKey: knowledgeKeys.detail(wsId, id ?? ""),
    queryFn: () => api.getKnowledge(id ?? ""),
    enabled: !!id,
  });
}

export function knowledgeSourcesOptions(wsId: string, id: string | null) {
  return queryOptions({
    queryKey: knowledgeKeys.sources(wsId, id ?? ""),
    queryFn: () => api.listKnowledgeSources(id ?? ""),
    enabled: !!id,
  });
}

export function knowledgeCandidatesOptions(
  wsId: string,
  params?: ListKnowledgeCandidatesParams,
) {
  return queryOptions({
    queryKey: knowledgeKeys.candidates(wsId, params),
    queryFn: () => api.listKnowledgeCandidates(params),
  });
}

export function knowledgeGovernanceFindingsOptions(
  wsId: string,
  params?: ListKnowledgeGovernanceFindingsParams,
) {
  return queryOptions({
    queryKey: knowledgeKeys.governanceFindings(wsId, params),
    queryFn: () => api.listKnowledgeGovernanceFindings(params),
  });
}

export function knowledgeAnalyticsOptions(
  wsId: string,
  params?: ListKnowledgeAnalyticsParams,
) {
  return queryOptions({
    queryKey: knowledgeKeys.analytics(wsId, params),
    queryFn: () => api.listKnowledgeAnalytics(params),
  });
}

export function knowledgeEffectOptions(
  wsId: string,
  params?: ListKnowledgeEffectParams,
) {
  return queryOptions({
    queryKey: knowledgeKeys.effect(wsId, params),
    queryFn: () => api.listKnowledgeEffect(params),
  });
}

export function knowledgeEffectSummaryOptions(
  wsId: string,
  params?: Omit<ListKnowledgeEffectParams, "limit" | "offset">,
) {
  return queryOptions({
    queryKey: knowledgeKeys.effectSummary(wsId, params),
    queryFn: () => api.getKnowledgeEffectSummary(params),
  });
}
