export type TaskStatus =
  | "queued"
  | "dispatched"
  | "running"
  | "completed"
  | "failed"
  | "cancelled";

export type TaskType = "issue" | "chat";

export type TaskTimeRange = "all" | "24h" | "7d" | "30d" | "custom";

export type ColumnId =
  | "agent"
  | "task"
  | "issue_name"
  | "issue_id"
  | "status"
  | "started"
  | "duration"
  | "cost"
  | "triggered_by";

export interface ColumnDef {
  id: ColumnId;
  label: string;
}

export const COLUMN_DEFS: ColumnDef[] = [
  { id: "agent", label: "Agent" },
  { id: "task", label: "Task" },
  { id: "issue_name", label: "Issue navn" },
  { id: "issue_id", label: "Issue ID" },
  { id: "status", label: "Status" },
  { id: "started", label: "Started" },
  { id: "duration", label: "Varighed" },
  { id: "cost", label: "Kost" },
  { id: "triggered_by", label: "Startet af" },
];

export const DEFAULT_VISIBLE_COLUMNS: Record<ColumnId, boolean> = {
  agent: true,
  task: true,
  issue_name: true,
  issue_id: true,
  status: true,
  started: true,
  duration: true,
  cost: true,
  triggered_by: false,
};

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
  cost_cents?: number;
  triggered_by_name?: string;
}

export interface TasksListResponse {
  tasks: CerebroTask[];
  total: number;
  limit: number;
  offset: number;
}

export interface TasksFilter {
  agentId: string | null;
  issueId: string | null;
  projectId: string | null;
  status: TaskStatus | null;
  type: TaskType | null;
  range: TaskTimeRange;
  customFrom: string | null;
  customTo: string | null;
  search: string;
  limit: number;
  offset: number;
}

export const DEFAULT_TASKS_FILTER: TasksFilter = {
  agentId: null,
  issueId: null,
  projectId: null,
  status: null,
  type: null,
  range: "24h",
  customFrom: null,
  customTo: null,
  search: "",
  limit: 50,
  offset: 0,
};

export function timeRangeToSinceISO(range: TaskTimeRange, customFrom: string | null, now = new Date()): string | null {
  if (range === "all") return null;
  if (range === "custom") return customFrom ?? null;
  const ms =
    range === "24h" ? 24 * 60 * 60 * 1000 : range === "7d" ? 7 * 24 * 60 * 60 * 1000 : 30 * 24 * 60 * 60 * 1000;
  return new Date(now.getTime() - ms).toISOString();
}
