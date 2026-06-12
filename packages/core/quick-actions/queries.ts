import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

export const quickActionKeys = {
  all: (wsId: string) => ["quick-actions", wsId] as const,
  list: (wsId: string) => [...quickActionKeys.all(wsId), "list"] as const,
};

export function quickActionListOptions(wsId: string) {
  return queryOptions({
    queryKey: quickActionKeys.list(wsId),
    queryFn: () => api.listQuickActions(),
    select: (data) => data.quick_actions,
  });
}
