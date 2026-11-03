import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import { knowledgeKeys } from "./queries";

export const knowledgePlatformKeys = {
  sources: (wsId: string) => [...knowledgeKeys.all(wsId), "sources"] as const,
  access: (wsId: string, key: string) => [...knowledgeKeys.all(wsId), "access", key] as const,
};

/** The Sources panel: owners and admins. Reads WeKnora through the server. */
export function knowledgeSourcesOptions(wsId: string) {
  return queryOptions({
    queryKey: knowledgePlatformKeys.sources(wsId),
    queryFn: () => api.listKnowledgeSources(),
  });
}

/** A base's visibility and grants: owners and admins. */
export function knowledgeAccessOptions(wsId: string, key: string) {
  return queryOptions({
    queryKey: knowledgePlatformKeys.access(wsId, key),
    queryFn: () => api.getKnowledgeBaseAccess(key),
    enabled: key.length > 0,
  });
}
