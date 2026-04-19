export type AgentStatus = "idle" | "working" | "blocked" | "error" | "offline";

export type AgentRuntimeMode = "local" | "cloud";

export type AgentVisibility = "workspace" | "private";

export interface RuntimeDevice {
  id: string;
  workspace_id: string;
  daemon_id: string | null;
  name: string;
  runtime_mode: AgentRuntimeMode;
  provider: string;
  status: "online" | "offline";
  device_info: string;
  metadata: Record<string, unknown>;
  owner_id: string | null;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentRuntime = RuntimeDevice;

export interface AgentRuntimeRef {
  id: string;
  name: string;
  status: "online" | "offline";
  runtime_mode: AgentRuntimeMode;
  provider: string;
  device_info: string;
  owner_id: string | null;
  last_used_at: string | null;
}

export interface RuntimeGroupOverride {
  id: string;
  group_id: string;
  runtime_id: string;
  runtime_name: string;
  starts_at: string;
  ends_at: string;
  created_by: string | null;
}

export interface RuntimeGroup {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  runtimes: AgentRuntimeRef[];
  active_override: RuntimeGroupOverride | null;
  member_agent_count: number;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface AgentRuntimeGroupRef {
  id: string;
  name: string;
  active_override: {
    runtime_id: string;
    runtime_name: string;
    ends_at: string;
  } | null;
}

export interface CreateRuntimeGroupRequest {
  name: string;
  description?: string;
  runtime_ids: string[];
}

export interface UpdateRuntimeGroupRequest {
  name?: string;
  description?: string;
  runtime_ids?: string[];
}

export interface SetRuntimeGroupOverrideRequest {
  runtime_id: string;
  ends_at: string;
}

export interface AgentTask {
  id: string;
  agent_id: string;
  runtime_id: string;
  issue_id: string;
  status: "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";
  priority: number;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  created_at: string;
}

export interface Agent {
  id: string;
  workspace_id: string;
  runtime_ids: string[];
  runtimes: AgentRuntimeRef[];
  groups: AgentRuntimeGroupRef[];
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  runtime_mode: AgentRuntimeMode;
  runtime_config: Record<string, unknown>;
  custom_env: Record<string, string>;
  custom_args: string[];
  custom_env_redacted: boolean;
  visibility: AgentVisibility;
  status: AgentStatus;
  max_concurrent_tasks: number;
  owner_id: string | null;
  skills: Skill[];
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

export interface CreateAgentRequest {
  name: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_ids: string[];
  group_ids?: string[];
  runtime_config?: Record<string, unknown>;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  visibility?: AgentVisibility;
  max_concurrent_tasks?: number;
}

export interface UpdateAgentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_ids?: string[];
  group_ids?: string[];
  runtime_config?: Record<string, unknown>;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  visibility?: AgentVisibility;
  status?: AgentStatus;
  max_concurrent_tasks?: number;
}

// Skills

export interface Skill {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  content: string;
  config: Record<string, unknown>;
  files: SkillFile[];
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface SkillFile {
  id: string;
  skill_id: string;
  path: string;
  content: string;
  created_at: string;
  updated_at: string;
}

export interface CreateSkillRequest {
  name: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface UpdateSkillRequest {
  name?: string;
  description?: string;
  content?: string;
  config?: Record<string, unknown>;
  files?: { path: string; content: string }[];
}

export interface SetAgentSkillsRequest {
  skill_ids: string[];
}

export type RuntimePingStatus = "pending" | "running" | "completed" | "failed" | "timeout";

export interface RuntimePing {
  id: string;
  runtime_id: string;
  status: RuntimePingStatus;
  output?: string;
  error?: string;
  duration_ms?: number;
  created_at: string;
  updated_at: string;
}

export interface IssueUsageSummary {
  total_input_tokens: number;
  total_output_tokens: number;
  total_cache_read_tokens: number;
  total_cache_write_tokens: number;
  task_count: number;
}

export interface RuntimeUsage {
  runtime_id: string;
  date: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
}

export interface RuntimeHourlyActivity {
  hour: number;
  count: number;
}

export type RuntimeUpdateStatus =
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "timeout";

export interface RuntimeUpdate {
  id: string;
  runtime_id: string;
  status: RuntimeUpdateStatus;
  target_version: string;
  output?: string;
  error?: string;
  created_at: string;
  updated_at: string;
}
