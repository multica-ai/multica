export type TaskStatus =
  | "queued"
  | "dispatched"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";

export type TaskType = "issue" | "chat";

export type TaskTimeRange = "all" | "24h" | "7d" | "30d";

export interface CerebroTask {
  task_id: string;
  agent_id: string;
  agent_name: string;
  agent_avatar_url?: string;
  task_title?: string;
  issue_id?: string;
  issue_title?: string;
  issue_number?: number;
  chat_session_id?: string;
  status: TaskStatus | string;
  dispatched_at?: string;
  started_at?: string;
  completed_at?: string;
  created_at: string;
}

export interface TasksListResponse {
  tasks: CerebroTask[];
  total: number;
  limit: number;
  offset: number;
}

export interface TasksFilter {
  agentId: string | null;
  status: TaskStatus | null;
  type: TaskType | null;
  range: TaskTimeRange;
  limit: number;
  offset: number;
}

export const DEFAULT_TASKS_FILTER: TasksFilter = {
  agentId: null,
  status: null,
  type: null,
  range: "all",
  limit: 50,
  offset: 0,
};

export function timeRangeToSinceISO(range: TaskTimeRange, now = new Date()): string | null {
  if (range === "all") return null;
  const ms =
    range === "24h" ? 24 * 60 * 60 * 1000 : range === "7d" ? 7 * 24 * 60 * 60 * 1000 : 30 * 24 * 60 * 60 * 1000;
  return new Date(now.getTime() - ms).toISOString();
}
