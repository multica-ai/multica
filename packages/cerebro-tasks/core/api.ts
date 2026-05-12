import { api } from "@multica/core/api";
import type { TasksFilter, TasksListResponse } from "./types";
import { timeRangeToSinceISO } from "./types";

export async function fetchCerebroTasks(filter: TasksFilter): Promise<TasksListResponse> {
  const since = timeRangeToSinceISO(filter.range);
  return api.getCerebroTasks<TasksListResponse>({
    agent_id: filter.agentId,
    status: filter.status,
    type: filter.type,
    since,
    limit: filter.limit,
    offset: filter.offset,
  });
}
