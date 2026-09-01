import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";
import type { ListMarketplaceTemplatesParams } from "../types";

export const marketplaceTemplateKeys = {
  all: (wsId: string) => ["workspaces", wsId, "marketplace-templates"] as const,
  list: (wsId: string, params: ListMarketplaceTemplatesParams) =>
    [...marketplaceTemplateKeys.all(wsId), "list", params] as const,
  detail: (wsId: string, id: string) =>
    [...marketplaceTemplateKeys.all(wsId), "detail", id] as const,
};

export function marketplaceTemplateListOptions(
  wsId: string,
  params: ListMarketplaceTemplatesParams = {},
) {
  return queryOptions({
    queryKey: marketplaceTemplateKeys.list(wsId, params),
    queryFn: () => api.listMarketplaceTemplates(params),
    enabled: !!wsId,
  });
}

export function marketplaceTemplateDetailOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: marketplaceTemplateKeys.detail(wsId, id),
    queryFn: () => api.getMarketplaceTemplate(id),
    enabled: !!wsId && !!id,
  });
}
