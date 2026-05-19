import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const wikiKeys = {
  all: (wsId: string) => ["wiki", wsId] as const,
  list: (wsId: string) => [...wikiKeys.all(wsId), "list"] as const,
  detail: (wsId: string, pageId: string) =>
    [...wikiKeys.all(wsId), "detail", pageId] as const,
  activity: (wsId: string, pageId: string) =>
    [...wikiKeys.all(wsId), "activity", pageId] as const,
};

export function wikiPageListOptions(wsId: string) {
  return queryOptions({
    queryKey: wikiKeys.list(wsId),
    queryFn: () => api.listWikiPages(),
    select: (data) => data.pages,
  });
}

export function wikiPageDetailOptions(wsId: string, pageId: string | null) {
  return queryOptions({
    queryKey: wikiKeys.detail(wsId, pageId ?? ""),
    queryFn: () => api.getWikiPage(pageId ?? ""),
    enabled: !!pageId,
  });
}

export function wikiPageActivityOptions(wsId: string, pageId: string | null) {
  return queryOptions({
    queryKey: wikiKeys.activity(wsId, pageId ?? ""),
    queryFn: () => api.listWikiPageActivities(pageId ?? ""),
    enabled: !!pageId,
    select: (data) => data.activities,
  });
}
