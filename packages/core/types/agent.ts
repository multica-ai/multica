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

// Coarse classifier set by the backend when a task transitions to "failed".
// Mirrors the migration-055 enum in agent_task_queue.failure_reason. Used by
// the agent presence derivation and the UI failure-message lookup.
export type TaskFailureReason =
  | "agent_error"
  | "timeout"
  | "runtime_offline"
  | "runtime_recovery"
  | "manual";

// One daily bucket for the Agents-list ACTIVITY sparkline. The back-end
// only returns days that had at least one completion; the front-end fills
// in missing days with zero when rendering the 7-bucket series. The series
// is anchored on completed_at (a task in flight contributes nothing).
export interface AgentActivityBucket {
  agent_id: string;
  // ISO timestamp at midnight UTC of the day.
  bucket_at: string;
  task_count: number;
  failed_count: number;
}

// 30-day total run count per agent, drives the Agents-list RUNS column.
export interface AgentRunCount {
  agent_id: string;
  run_count: number;
}

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
  // Empty string when the task is not in a failed state (the backend uses
  // `omitempty`, so the field may also be missing on non-failed tasks).
  failure_reason?: TaskFailureReason | "";
  created_at: string;
  /** Non-empty when the task was spawned from a chat session. */
  chat_session_id?: string;
  /** Non-empty when the task was spawned by an autopilot run. */
  autopilot_run_id?: string;
  /** Set when this task was created as an auto-retry of a parent task. */
  parent_task_id?: string;
  /** 1-based attempt counter; >1 means this is a retry. */
  attempt?: number;
  /** Set when an issue comment triggered this task (@mention or assignee comment). */
  trigger_comment_id?: string;
  /**
   * Canonical short description of what triggered this task — snapshot
   * taken at creation time. For comment-triggered tasks it's the
   * comment text (truncated to ~200 chars); for autopilot it's the
   * autopilot title; NULL for direct assignments and chat tasks.
   * Persists even if the source comment / autopilot is later edited
   * or deleted.
   */
  trigger_summary?: string;
  /**
   * Server-computed source discriminator used by the activity row to label
   * tasks that have no linked issue (so e.g. quick-create tasks render
   * with a meaningful title instead of falling through to "Untracked").
   */
  kind?: "comment" | "autopilot" | "chat" | "quick_create" | "direct";
  /**
   * Local working directory pinned for this task by the daemon. Empty until
   * the daemon reports a work_dir (typically once execution starts).
   */
  work_dir?: string;
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
  custom_env_redacted: boolean;
  visibility: AgentVisibility;
  status: AgentStatus;
  max_concurrent_tasks: number;
  model: string;
  owner_id: string | null;
  skills: AgentSkillSummary[];
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

/**
 * Minimal skill shape embedded in an Agent payload (`GET /api/agents`,
 * `GET /api/agents/:id`). Only id/name/description are populated — the
 * agent list batch query joins exactly those three columns. For full skill
 * info, use `GET /api/agents/:id/skills` (returns `SkillSummary[]`) or
 * `GET /api/skills/:id` (returns the full `Skill`).
 */
export interface AgentSkillSummary {
  id: string;
  name: string;
  description: string;
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
  visibility?: AgentVisibility;
  status?: AgentStatus;
  max_concurrent_tasks?: number;
  model?: string;
}

// Skills

/**
 * Lightweight skill shape returned by list endpoints (`GET /api/skills`,
 * `GET /api/agents/:id/skills`). The full SKILL.md `content` is intentionally
 * omitted — bodies routinely run 50–200KB each and shipping them in list
 * payloads tripped CLI timeouts on high-latency links (GH
 * multica-ai/multica#2174). Use `Skill` from a detail endpoint when you need
 * the body. For skills embedded in an `Agent` payload see `AgentSkillSummary`.
 */
export interface SkillSummary {
  id: string;
  workspace_id: string;
  name: string;
  description: string;
  config: Record<string, unknown>;
  created_by: string | null;
  created_at: string;
  updated_at: string;
}

export interface Skill extends SkillSummary {
  content: string;
  files: SkillFile[];
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

// One (agent, model) row of the "Cost by agent" tab on the runtime detail
// page. Model stays on the wire because cost is computed client-side from
// a per-model pricing table — the client groups these rows by agent_id and
// sums cost per agent across models.
export interface RuntimeUsageByAgent {
  agent_id: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  task_count: number;
}

// One (hour, model) row for the "By hour" tab; hour ∈ 0..23. Hours with
// zero activity are omitted by the server; the client fills the gap to
// render a continuous axis. Model preserved for client-side cost math.
export interface RuntimeUsageByHour {
  hour: number;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  task_count: number;
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
