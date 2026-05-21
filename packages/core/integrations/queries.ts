import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const integrationKeys = {
  all: (wsId: string) => ["integrations", wsId] as const,
  list: (wsId: string) => [...integrationKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...integrationKeys.all(wsId), "detail", id] as const,
  links: (wsId: string, id: string) =>
    [...integrationKeys.all(wsId), "links", id] as const,
};

export function integrationListOptions(wsId: string) {
  return queryOptions({
    queryKey: integrationKeys.list(wsId),
    queryFn: () => api.listIntegrations(),
  });
}

export function integrationDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: integrationKeys.detail(wsId, id),
    queryFn: () => api.getIntegration(id),
  });
}

export function integrationLinksOptions(wsId: string, integrationId: string) {
  return queryOptions({
    queryKey: integrationKeys.links(wsId, integrationId),
    queryFn: () => api.listExternalLinks(integrationId),
  });
}
