import { api } from "@multica/core/api";
import type { TasksFilter, TasksListResponse } from "./types";
import { timeRangeToSinceISO } from "./types";

export async function fetchCerebroTasks(filter: TasksFilter): Promise<TasksListResponse> {
  const since = timeRangeToSinceISO(filter.range, filter.customFrom);
  const until = filter.range === "custom" ? (filter.customTo ?? null) : null;
  return api.getCerebroTasks<TasksListResponse>({
    agent_id: filter.agentId,
    issue_id: filter.issueId,
    project_id: filter.projectId,
    status: filter.status,
    type: filter.type,
    since,
    until,
    limit: filter.limit,
    offset: filter.offset,
    q: filter.search || null,
  });
}
