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
  launch_header: string;
  status: "online" | "offline";
  device_info: string;
  metadata: Record<string, unknown>;
  owner_id: string | null;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentRuntime = RuntimeDevice;

export interface AgentTask {
  id: string;
  agent_id: string;
  runtime_id: string;
  // Empty string ("") when the task has no linked issue — either chat- or
  // autopilot-spawned. Check chat_session_id / autopilot_run_id to tell
  // which source produced it.
  issue_id: string;
  status: "queued" | "dispatched" | "running" | "completed" | "failed" | "cancelled";
  priority: number;
  dispatched_at: string | null;
  started_at: string | null;
  completed_at: string | null;
  result: unknown;
  error: string | null;
  created_at: string;
  /** Non-empty when the task was spawned from a chat session. */
  chat_session_id?: string;
  /** Non-empty when the task was spawned by an autopilot run. */
  autopilot_run_id?: string;
}

export interface Agent {
  id: string;
  workspace_id: string;
  runtime_id: string;
  name: string;
  description: string;
  instructions: string;
  avatar_url: string | null;
  runtime_mode: AgentRuntimeMode;
  runtime_config: Record<string, unknown>;
  custom_env: Record<string, string>;
  custom_args: string[];
  mcp_config: unknown | null;
  custom_env_redacted: boolean;
  mcp_config_redacted: boolean;
  visibility: AgentVisibility;
  status: AgentStatus;
  max_concurrent_tasks: number;
  model: string;
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
  runtime_id: string;
  runtime_config?: Record<string, unknown>;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  mcp_config?: unknown;
  visibility?: AgentVisibility;
  max_concurrent_tasks?: number;
  model?: string;
  /** Optional template slug used by the onboarding agent picker. Surfaced
   *  as the `template` property on the `agent_created` PostHog event. */
  template?: string;
}

export interface UpdateAgentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id?: string;
  runtime_config?: Record<string, unknown>;
  custom_env?: Record<string, string>;
  custom_args?: string[];
  mcp_config?: unknown;
  visibility?: AgentVisibility;
  status?: AgentStatus;
  max_concurrent_tasks?: number;
  model?: string;
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

export interface RuntimeModel {
  id: string;
  label: string;
  provider?: string;
  default?: boolean;
}

export type RuntimeModelListStatus =
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "timeout";

export interface RuntimeModelListRequest {
  id: string;
  runtime_id: string;
  status: RuntimeModelListStatus;
  models?: RuntimeModel[];
  supported: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

// Result shape returned by resolveRuntimeModels — includes the
// "supported" bit so the UI can distinguish "no models discovered"
// from "provider does not honour per-agent model selection".
export interface RuntimeModelsResult {
  models: RuntimeModel[];
  supported: boolean;
}

export type RuntimeLocalSkillStatus =
  | "pending"
  | "running"
  | "completed"
  | "failed"
  | "timeout";

export interface RuntimeLocalSkillSummary {
  key: string;
  name: string;
  description?: string;
  source_path: string;
  provider: string;
  file_count: number;
}

export interface RuntimeLocalSkillListRequest {
  id: string;
  runtime_id: string;
  status: RuntimeLocalSkillStatus;
  skills?: RuntimeLocalSkillSummary[];
  supported: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRuntimeLocalSkillImportRequest {
  skill_key: string;
  name?: string;
  description?: string;
}

export interface RuntimeLocalSkillImportRequest {
  id: string;
  runtime_id: string;
  skill_key: string;
  name?: string;
  description?: string;
  status: RuntimeLocalSkillStatus;
  skill?: Skill;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeLocalSkillsResult {
  skills: RuntimeLocalSkillSummary[];
  supported: boolean;
}

export interface RuntimeLocalSkillImportResult {
  skill: Skill;
}
