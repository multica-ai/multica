import { queryOptions, useMutation, useQueryClient } from "@tanstack/react-query";
import { api } from "../api";
import { proposalKeys } from "./proposals";
import type { Brief, ListBriefsParams, PublishBriefRequest } from "./brief-schema";

export const briefKeys = {
  all: (wsId: string) => ["briefs", wsId] as const,
  list: (wsId: string) => [...briefKeys.all(wsId), "list"] as const,
  /** Full key for a filtered list; the unfiltered list shares the prefix. */
  listFiltered: (wsId: string, params: ListBriefsParams = {}) =>
    [...briefKeys.list(wsId), params.kind ?? "", params.limit ?? 0] as const,
  detail: (wsId: string, id: string) => [...briefKeys.all(wsId), "detail", id] as const,
};

export function briefListOptions(wsId: string, params: ListBriefsParams = {}) {
  return queryOptions({
    queryKey: briefKeys.listFiltered(wsId, params),
    queryFn: () => api.listBriefs(params),
  });
}

export function briefDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: briefKeys.detail(wsId, id),
    queryFn: () => api.getBrief(id),
    enabled: !!id,
  });
}

/**
 * Publishes a brief ("Generate now" is a manual autopilot run; this is for an
 * admin writing one by hand). Awaits the server: heading and size validation
 * happen there and a refusal (400) carries a message written for the author.
 */
export function usePublishBrief(wsId: string) {
  const qc = useQueryClient();
  return useMutation<Brief, Error, PublishBriefRequest>({
    mutationFn: (data) => api.publishBrief(data),
    onSettled: () => {
      qc.invalidateQueries({ queryKey: briefKeys.all(wsId) });
      qc.invalidateQueries({ queryKey: proposalKeys.all(wsId) });
    },
  });
}
