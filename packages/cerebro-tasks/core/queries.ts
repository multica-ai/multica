import { queryOptions } from "@tanstack/react-query";
import { fetchCerebroTasks } from "./api";
import type { TasksFilter } from "./types";

export const cerebroTasksKeys = {
  all: (wsId: string) => ["cerebro", "tasks", wsId] as const,
  list: (wsId: string, filter: TasksFilter) =>
    [
      ...cerebroTasksKeys.all(wsId),
      "list",
      filter.agentId,
      filter.issueId,
      filter.projectId,
      filter.status,
      filter.type,
      filter.range,
      filter.customFrom,
      filter.customTo,
      filter.search,
      filter.limit,
      filter.offset,
    ] as const,
};

export function cerebroTasksListOptions(wsId: string, filter: TasksFilter) {
  return queryOptions({
    queryKey: cerebroTasksKeys.list(wsId, filter),
    queryFn: () => fetchCerebroTasks(filter),
    enabled: !!wsId,
    staleTime: 15 * 1000,
    refetchOnWindowFocus: true,
    placeholderData: (prev) => prev,
  });
}
