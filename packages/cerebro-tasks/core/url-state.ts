import {
  DEFAULT_TASKS_FILTER,
  type GroupBy,
  type TaskStatus,
  type TaskTimeRange,
  type TaskType,
  type TasksFilter,
} from "./types";

const STATUSES = new Set<TaskStatus>([
  "queued",
  "dispatched",
  "running",
  "completed",
  "failed",
  "cancelled",
]);

const TYPES = new Set<TaskType>(["issue", "chat"]);
const RANGES = new Set<TaskTimeRange>(["all", "24h", "7d", "30d", "custom"]);
const GROUP_BY = new Set<GroupBy>(["none", "issue", "project", "parent_issue"]);

export function parseTasksFilterFromSearchParams(params: URLSearchParams): Partial<TasksFilter> {
  const limit = parsePositiveInt(params.get("limit"));
  const offset = parseNonNegativeInt(params.get("offset"));
  const status = parseEnum(params.get("status"), STATUSES);
  const type = parseEnum(params.get("type"), TYPES);
  const range = parseEnum(params.get("range"), RANGES);
  const groupBy = parseEnum(params.get("group"), GROUP_BY);

  return {
    agentId: params.get("agent") || null,
    issueId: params.get("issue") || null,
    projectId: params.get("project") || null,
    status,
    type,
    range: range ?? DEFAULT_TASKS_FILTER.range,
    customFrom: params.get("from") || null,
    customTo: params.get("to") || null,
    search: params.get("q") ?? "",
    groupBy: groupBy ?? DEFAULT_TASKS_FILTER.groupBy,
    limit: limit ?? DEFAULT_TASKS_FILTER.limit,
    offset: offset ?? DEFAULT_TASKS_FILTER.offset,
  };
}

export function serializeTasksFilter(filter: TasksFilter): string {
  const params = new URLSearchParams();

  if (filter.status) params.set("status", filter.status);
  if (filter.agentId) params.set("agent", filter.agentId);
  if (filter.issueId) params.set("issue", filter.issueId);
  if (filter.projectId) params.set("project", filter.projectId);
  if (filter.type) params.set("type", filter.type);
  if (filter.range !== DEFAULT_TASKS_FILTER.range) params.set("range", filter.range);
  if (filter.range === "custom" && filter.customFrom) params.set("from", filter.customFrom);
  if (filter.range === "custom" && filter.customTo) params.set("to", filter.customTo);
  if (filter.search.trim()) params.set("q", filter.search.trim());
  if (filter.groupBy !== DEFAULT_TASKS_FILTER.groupBy) params.set("group", filter.groupBy);
  if (filter.limit !== DEFAULT_TASKS_FILTER.limit) params.set("limit", String(filter.limit));
  if (filter.offset !== DEFAULT_TASKS_FILTER.offset) params.set("offset", String(filter.offset));

  return params.toString();
}

export function tasksFilterPath(pathname: string, filter: TasksFilter): string {
  const query = serializeTasksFilter(filter);
  return query ? `${pathname}?${query}` : pathname;
}

function parsePositiveInt(raw: string | null): number | null {
  const n = parseNonNegativeInt(raw);
  return n && n > 0 ? n : null;
}

function parseNonNegativeInt(raw: string | null): number | null {
  if (!raw) return null;
  const n = Number.parseInt(raw, 10);
  if (!Number.isFinite(n) || n < 0) return null;
  return n;
}

function parseEnum<T extends string>(raw: string | null, values: Set<T>): T | null {
  if (!raw) return null;
  return values.has(raw as T) ? (raw as T) : null;
}
