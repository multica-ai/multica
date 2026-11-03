import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListInitiativesParams } from "./types";

export const pipelineKeys = {
  all: (wsId: string) => ["pipelines", wsId] as const,
  list: (wsId: string) => [...pipelineKeys.all(wsId), "list"] as const,
};

export const initiativeKeys = {
  all: (wsId: string) => ["initiatives", wsId] as const,
  list: (wsId: string, params: ListInitiativesParams = {}) =>
    [
      ...initiativeKeys.all(wsId),
      "list",
      params.pipeline_id ?? "",
      params.status ?? "",
      params.owner_squad_id ?? "",
    ] as const,
  detail: (wsId: string, id: string) => [...initiativeKeys.all(wsId), "detail", id] as const,
};

/** The workspace's pipelines, default first when the server lists it so. */
export function pipelineListOptions(wsId: string) {
  return queryOptions({
    queryKey: pipelineKeys.list(wsId),
    queryFn: () => api.listPipelines(),
    enabled: !!wsId,
  });
}

export function initiativeListOptions(wsId: string, params: ListInitiativesParams = {}) {
  return queryOptions({
    queryKey: initiativeKeys.list(wsId, params),
    queryFn: () => api.listInitiatives(params),
    enabled: !!wsId,
  });
}

export function initiativeDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: initiativeKeys.detail(wsId, id),
    queryFn: () => api.getInitiative(id),
    enabled: !!wsId && !!id,
  });
}
