import { queryOptions } from "@tanstack/react-query";
import { api } from "@/data/api";

export const skillKeys = {
  all: (wsId: string | null) => ["skills", wsId] as const,
  list: (wsId: string | null) => [...skillKeys.all(wsId), "list"] as const,
  detail: (wsId: string | null, id: string) =>
    [...skillKeys.all(wsId), "detail", id] as const,
};

export const skillListOptions = (wsId: string | null) =>
  queryOptions({
    queryKey: skillKeys.list(wsId),
    queryFn: ({ signal }) => api.listSkills({ signal }),
    enabled: !!wsId,
  });

export const skillDetailOptions = (wsId: string | null, id: string) =>
  queryOptions({
    queryKey: skillKeys.detail(wsId, id),
    queryFn: ({ signal }) => api.getSkill(id, { signal }),
    enabled: !!wsId && !!id,
  });
