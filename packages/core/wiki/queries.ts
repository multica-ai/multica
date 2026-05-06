import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const wikiKeys = {
  all: (wsId: string) => ["workspaces", wsId, "wiki"] as const,
  list: (wsId: string) => [...wikiKeys.all(wsId), "list"] as const,
  detail: (wsId: string, id: string) =>
    [...wikiKeys.all(wsId), "detail", id] as const,
};

export function wikiListOptions(wsId: string) {
  return queryOptions({
    queryKey: wikiKeys.list(wsId),
    queryFn: () => api.listWikiPages(),
    select: (data) => data.pages,
  });
}

export function wikiPageOptions(wsId: string, id: string) {
  return queryOptions({
    queryKey: wikiKeys.detail(wsId, id),
    queryFn: () => api.getWikiPage(id),
  });
}
