import { queryOptions } from "@tanstack/react-query";
import { api } from "../api";

/**
 * Quick action catalog queries (MUL-5465).
 *
 * ONE projection, shared by settings and the issue sidebar. There used to be a
 * second "runnable only" projection that filtered by invoke permission; it was
 * removed because it made two people looking at the same issue see different
 * sidebars with no explanation. Permission is now answered once, at run time.
 */
export const quickActionKeys = {
  all: (wsId: string) => ["quick-actions", wsId] as const,
  list: (wsId: string, includeArchived = false) =>
    [...quickActionKeys.all(wsId), "list", includeArchived] as const,
};

export function quickActionListOptions(wsId: string, includeArchived = false) {
  return queryOptions({
    queryKey: quickActionKeys.list(wsId, includeArchived),
    queryFn: () => api.listQuickActions({ includeArchived }),
    select: (data) => data.quick_actions,
  });
}
