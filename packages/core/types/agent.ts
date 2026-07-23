export type AgentStatus = "idle" | "working" | "blocked" | "error" | "offline";

export type AgentRuntimeMode = "local" | "cloud";

export type AgentVisibility = "workspace" | "private";

// ---------------------------------------------------------------------------
// Agent invocation permissions (MUL-3963)
//
// `permission_mode` + `invocation_targets` are the AUTHORITATIVE gate for who
// may TRIGGER / assign / @mention / chat an agent. The legacy `visibility`
// field REMAINS but is now DERIVED on the backend from these two: a
// `public_to` agent WITH a workspace target maps to `visibility: "workspace"`;
// everything else (private, or public_to scoped only to member/team targets)
// maps to `visibility: "private"`.
//
// Invocation semantics:
//   - owner: always
//   - permission_mode "private": ONLY the owner (workspace admins no longer
//     bypass — the key behavior change vs the old visibility model)
//   - permission_mode "public_to" + workspace target: any workspace member
//   - permission_mode "public_to" + member target: only the matching user
//   - team target: reserved, INERT in v1 (never grants)
// ---------------------------------------------------------------------------

export type AgentPermissionMode = "private" | "public_to";

/**
 * A single invocation grant on an agent. `target_id` is `null` for the
 * workspace target (the grant covers every workspace member); it carries the
 * member / team id for the scoped grants.
 */
export interface AgentInvocationTarget {
  target_type: "workspace" | "member" | "team";
  target_id: string | null;
}

/**
 * Wire shape for invocation targets on CREATE / UPDATE requests. For a
 * workspace target the client may omit `target_id` (the backend fills the
 * workspace id); member / team targets REQUIRE it.
 */
export interface AgentInvocationTargetInput {
  target_type: "workspace" | "member" | "team";
  target_id?: string;
}

// Runtime visibility is a separate axis from agent visibility — different
// vocabulary because it gates a different action. "private" (default) means
// only the runtime owner and workspace admins can bind agents to it;
// "public" opens binding to any workspace member. Older backends that
// haven't shipped MUL-2062 omit the field; the consumer must default to
// "private" so the strictest behavior is the fallback.
export type RuntimeVisibility = "private" | "public";

export interface RuntimeDevice {
  id: string;
  workspace_id: string;
  daemon_id: string | null;
  name: string;
  /**
   * Optional user-set display name override (MUL-4217). Overrides `name` for
   * display; the daemon never writes it, so it survives heartbeats. Older
   * backends omit the field — consumers must treat missing / empty as "use
   * name" (see runtimeDisplayName).
   */
  custom_name?: string | null;
  runtime_mode: AgentRuntimeMode;
  provider: string;
  launch_header: string;
  status: "online" | "offline";
  device_info: string;
  metadata: Record<string, unknown>;
  owner_id: string | null;
  /** Defaults to "private" when the backend predates the visibility flag. */
  visibility: RuntimeVisibility;
  /**
   * The custom runtime profile this registered runtime was launched from,
   * or `null` for a built-in protocol family. The UI uses this to stamp a
   * "Built-in" vs "Custom" badge on the runtime row. Older backends that
   * predate the custom-runtime feature omit the field; consumers must treat
   * a missing value as `null` (built-in).
   */
  profile_id?: string | null;
  last_seen_at: string | null;
  created_at: string;
  updated_at: string;
}

export type AgentRuntime = RuntimeDevice;

// ---------------------------------------------------------------------------
// Custom runtime profiles (MUL-3284)
//
// A RuntimeProfile is a workspace-level *definition* of a custom runtime
// backend — distinct from a RuntimeDevice, which is a daemon-registered
// *instance*. An admin authors a profile (display name + base protocol
// family + the CLI command to launch), and daemons can then register
// runtimes against it; those instances carry `profile_id` pointing back here.
// ---------------------------------------------------------------------------

// The fixed allow-list of base protocol families a custom runtime can wrap.
// These are the only backends the create flow may select; the server rejects
// anything else with 400. Kept as a const tuple so the union type is derived
// from the single source of truth.
export const RUNTIME_PROFILE_PROTOCOL_FAMILIES = [
  "claude",
  "codebuddy",
  "codex",
  "copilot",
  "opencode",
  "deveco",
  "openclaw",
  "hermes",
  "pi",
  "cursor",
  "kimi",
  "kiro",
  "antigravity",
  "qoder",
  "traecli",
  "grok",
  "qwen",
] as const;

export type RuntimeProtocolFamily =
  (typeof RUNTIME_PROFILE_PROTOCOL_FAMILIES)[number];

// Profile visibility mirrors RuntimeVisibility's vocabulary but uses the
// workspace/private axis the server documents for profiles.
export type RuntimeProfileVisibility = "workspace" | "private";

export interface RuntimeProfile {
  id: string;
  workspace_id: string;
  display_name: string;
  protocol_family: RuntimeProtocolFamily;
  command_name: string;
  description: string | null;
  fixed_args: string[];
  visibility: RuntimeProfileVisibility;
  created_by: string | null;
  enabled: boolean;
  created_at: string;
  updated_at: string;
}

// POST body. `protocol_family` is required and immutable after creation.
// Optional fields are omitted entirely when unset (never sent as null/empty)
// so the server applies its own defaults.
export interface CreateRuntimeProfileRequest {
  display_name: string;
  protocol_family: RuntimeProtocolFamily;
  command_name: string;
  description?: string;
  fixed_args?: string[];
  visibility?: RuntimeProfileVisibility;
  enabled?: boolean;
}

// PATCH body — every field optional; `protocol_family` is intentionally
// absent because it is immutable.
export interface UpdateRuntimeProfileRequest {
  display_name?: string;
  command_name?: string;
  description?: string | null;
  fixed_args?: string[];
  visibility?: RuntimeProfileVisibility;
  enabled?: boolean;
}

// Coarse classifier set by the backend when a task transitions to "failed".
// Mirrors the migration-055 enum in agent_task_queue.failure_reason. Used by
// the agent presence derivation and the UI failure-message lookup.
export type TaskFailureReason =
  | "agent_error"
  | "timeout"
  | "codex_semantic_inactivity"
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

/**
 * A departed-member-safe user ref resolved from the global user table. `name` /
 * `email` / `avatar_url` are absent until the server hydrates them (present on
 * user-facing task surfaces). Render defensively — fall back to a generic label
 * when only `id` is available. See MUL-4302 §9.
 */
export interface AttributionUser {
  id: string;
  name?: string;
  email?: string;
  avatar_url?: string;
}

/** The kind-tagged handle to a run's direct cause (comment, autopilot run, ...). */
export interface TaskEvidence {
  kind: string;
  ref_id: string;
}

/**
 * The resolved accountable-human provenance of an agent run (MUL-4302 §9). Free-text
 * `source` (server may add new levels), so switch on it with a default branch.
 */
export interface TaskAttribution {
  /**
   * Waterfall level that resolved the accountable human:
   * `direct_human` | `delegation` | `comment_source` | `rule_owner` |
   * `owner_fallback` | `backfill` | `unattributed`. Never blank.
   */
  source: string;
  /** False for degraded sources (owner_fallback / backfill / unattributed). */
  precise: boolean;
  /** The accountable human ("on behalf of"). Absent when unattributed. */
  initiator?: AttributionUser;
  /** The authorization human; absent for autopilot runs (rule_owner / owner_fallback). */
  originator?: AttributionUser;
  /** The direct cause of the run, for a jump-to-evidence affordance. */
  evidence?: TaskEvidence;
  rule_version_id?: string;
  delegated_from_task_id?: string;
  retry_of_task_id?: string;
  rerun_of_task_id?: string;
}

export interface AgentTask {
  id: string;
  agent_id: string;
  runtime_id: string;
  // Empty string ("") when the task has no linked issue — either chat- or
  // autopilot-spawned. Check chat_session_id / autopilot_run_id to tell
  // which source produced it.
  issue_id: string;
  // `waiting_local_directory` is the daemon-emitted hold state for the
  // local_directory flow: a task that has been dispatched but is parked
  // because another task currently owns the same on-disk path lock.
  // Treated as an active (non-terminal) state alongside queued/dispatched/
  // running by every consumer that buckets tasks into "active vs done".
  status:
    | "queued"
    | "dispatched"
    | "waiting_local_directory"
    | "running"
    | "completed"
    | "failed"
    | "cancelled";
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
   * Earlier comment IDs folded into this run before it was claimed. This does
   * not include `trigger_comment_id`, which remains the run's newest trigger.
   * Their unique union is the queued coverage plan; claimed-task consumers
   * should prefer `delivered_comment_ids` when that receipt is present. Omitted
   * by older backends and for runs that were not merged.
   */
  coalesced_comment_ids?: string[];
  /**
   * Comment IDs actually embedded in the task's latest claim response. Once a
   * task has left queued state this is the authoritative coverage receipt.
   * Omitted by older backends, where consumers may fall back to the planned
   * trigger/coalesced union; an explicitly empty array is still authoritative.
   */
  delivered_comment_ids?: string[];
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
   * Handoff instruction the assigner attached when starting this run (MUL-3375).
   * Present only on assignment-triggered runs that carried a note; the execution
   * log shows it inline as the trigger reason. Absent (legacy / no note) falls
   * back to the generic "initial run" label.
   */
  handoff_note?: string;
  /**
   * Server-computed source discriminator used by the activity row to label
   * tasks that have no linked issue (so e.g. quick-create tasks render
   * with a meaningful title instead of falling through to "Untracked").
   */
  kind?: "comment" | "autopilot" | "chat" | "quick_create" | "direct";
  /**
   * Local working directory pinned for this task by the daemon. Empty until
   * the daemon reports a work_dir (typically once execution starts). This is
   * the canonical absolute path the agent runs in; UI surfaces should prefer
   * `relative_work_dir` to avoid leaking the user's home directory.
   */
  work_dir?: string;
  /**
   * Privacy-safe display form of `work_dir`, derived on the server. For
   * standard tasks the daemon's workspaces root has been stripped off
   * (`<wsUUID>/<taskShort>/workdir`); for local_directory tasks where the
   * path lives outside that layout, the server strips recognised home
   * prefixes (`/Users/<name>/`, `/home/<name>/`, `<drive>:/Users/<name>/`)
   * and otherwise falls back to the basename so neither the home directory
   * nor the username leak into the UI. Older backends omit the field —
   * render it conditionally and never render `work_dir` raw (not even in
   * a tooltip / `title` / `aria-label`, since the goal is that screen
   * shares and screenshots also stay safe).
   */
  relative_work_dir?: string;
  /**
   * Resolved accountable-human provenance of this run (MUL-4302 §9): who it ran
   * "on behalf of", how that was resolved, and the evidence/lineage. Present on
   * user-facing task surfaces; older backends omit it — render conditionally.
   */
  attribution?: TaskAttribution;
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
  custom_args: string[];
  /**
   * Coarse metadata signalling whether the agent has any custom env
   * vars configured, without exposing the keys or values. Reads of
   * the real map go through the dedicated `GET /api/agents/{id}/env`
   * endpoint (owner/admin only, audited). MUL-2600.
   *
   * Optional in the type so older backends (pre-MUL-2600) that omit
   * the field don't crash the renderer; downstream code should treat
   * `undefined` as "unknown — assume no env" rather than "definitely
   * has env".
   */
  has_custom_env?: boolean;
  /**
   * Number of keys in the agent's custom_env map. Always present
   * alongside `has_custom_env`. Treat `undefined` as zero. MUL-2600.
   */
  custom_env_key_count?: number;
  /**
   * MCP server configuration forwarded to runtimes that consume
   * `agent.mcp_config` (see providerSupportsMcpConfig). Each backend
   * materialises it in the runtime-native place: Claude flags, Codex
   * config.toml, ACP session params, OpenCode env config, OpenClaw
   * wrapper config, etc. `null` (or the field omitted on legacy backends)
   * means no managed config; the daemon falls back to the CLI's own
   * default. MUL-2764.
   *
   * When the caller can't see secrets (an agent actor, or a non-owner
   * non-admin), the server replaces the value with `null` and sets
   * `mcp_config_redacted` to true so the UI can render a "configured
   * but hidden" state without exposing potentially sensitive fields.
   */
  mcp_config?: unknown | null;
  /**
   * True when the server stripped `mcp_config` from this response
   * because the caller lacks permission to see secrets. The UI uses
   * this to distinguish "no config" (`mcp_config === null &&
   * !mcp_config_redacted`) from "config exists but you can't see it".
   * Older backends omit this field; treat `undefined` as false.
   */
  mcp_config_redacted?: boolean;
  /**
   * The subset of Composio toolkit slugs this agent is allowed to mount as
   * MCP servers at task dispatch — but only when the run originator is the
   * agent owner (MUL-3869 / MUL-3721). `null`/`[]`/omitted all mean "no
   * overlay regardless of who triggers". Owner-only data: the server hands
   * it through verbatim to the owner and redacts it to `undefined` +
   * `composio_toolkit_allowlist_redacted=true` for everyone else (same
   * contract as `mcp_config`). Treat `undefined` as "unknown — assume none".
   */
  composio_toolkit_allowlist?: string[];
  /**
   * True when the server stripped `composio_toolkit_allowlist` from this
   * response because the caller is not the agent owner. The MCP tab is
   * creator-only so a redacted value should never reach the editor, but the
   * UI renders a "hidden" fallback defensively. Older backends omit this
   * field; treat `undefined` as false.
   */
  composio_toolkit_allowlist_redacted?: boolean;
  visibility: AgentVisibility;
  /**
   * Authoritative invocation permission mode (MUL-3963). The `visibility`
   * field above is DERIVED from this on the backend. The current backend
   * always returns this field.
   */
  permission_mode: AgentPermissionMode;
  /**
   * Invocation grants backing `permission_mode === "public_to"` (empty for a
   * private agent). See `AgentInvocationTarget`.
   */
  invocation_targets: AgentInvocationTarget[];
  status: AgentStatus;
  max_concurrent_tasks: number;
  model: string;
  /**
   * Runtime-native reasoning/effort token (e.g. Claude's
   * `low|medium|high|xhigh|max`, Codex's
   * `none|minimal|low|medium|high|xhigh|max|ultra`). Empty string means "no
   * override": the backend omits the effort flag and the upstream CLI
   * config / built-in default decides at run time. The picker is
   * per-runtime per-model — the API never normalises across providers.
   * Older backends omit this field entirely; treat undefined as ""
   * (MUL-2339).
   */
  thinking_level?: string;
  owner_id: string | null;
  skills: AgentSkillSummary[];
  /** Runtime-local skills this agent must not inherit. Older servers omit it. */
  disabled_runtime_skills?: DisabledRuntimeSkill[];
  created_at: string;
  updated_at: string;
  archived_at: string | null;
  archived_by: string | null;
}

export interface DisabledRuntimeSkill {
  runtime_id: string;
  provider: string;
  root: "provider" | "universal" | "plugin";
  key: string;
  name?: string;
  plugin?: string;
}

export interface SetAgentRuntimeSkillEnabledRequest {
  runtime_id: string;
  root: "provider" | "universal" | "plugin";
  key: string;
  name: string;
  plugin?: string;
  enabled: boolean;
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
	/** Older servers omit this field; consumers must treat that as enabled. */
	enabled?: boolean;
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
  /**
   * Invocation permission mode (MUL-3963). When present it is authoritative;
   * when absent the backend maps the legacy `visibility` field
   * (private -> private, workspace -> public_to + workspace target). On
   * UPDATE, permission changes are OWNER-ONLY (the backend silently ignores
   * these fields from non-owner admins).
   */
  permission_mode?: AgentPermissionMode;
  /** Invocation grants — see `AgentInvocationTargetInput`. */
  invocation_targets?: AgentInvocationTargetInput[];
  max_concurrent_tasks?: number;
  model?: string;
  /** Optional runtime-native reasoning/effort token. See `Agent.thinking_level`. */
  thinking_level?: string;
  /** Optional template slug used by the onboarding agent picker. Surfaced
   *  as the `template` property on the `agent_created` PostHog event. */
  template?: string;
  /** Workspace skill IDs attached atomically with the agent row. */
  skill_ids?: string[];
}

export interface AgentBuilderSession {
  session_id: string;
  builder_agent_id: string;
  runtime_id: string;
}

/** Result of rebinding a live builder conversation to another runtime.
 *  `runtime_id` is the runtime the server actually bound — the caller must
 *  wait for it before showing the new runtime as selected. */
export interface AgentBuilderRuntimeSwitch {
  runtime_id: string;
}

/** Agent template summary — fields needed by the picker grid. Does NOT
 *  include `instructions` to keep the list payload small; the detail
 *  endpoint or the create flow returns the full template body. */
export interface AgentTemplateSummary {
  slug: string;
  name: string;
  description: string;
  /** Optional grouping for the picker UI ("Engineering" / "Writing" / …). */
  category?: string;
  /** Optional lucide-react icon name (e.g. "Search"). Frontend falls back
   *  to a generic icon when empty. */
  icon?: string;
  /** Optional semantic color token for the icon badge — one of "info" /
   *  "success" / "warning" / "primary" / "secondary". Frontend has a
   *  static class map so Tailwind can JIT-scan all variants. */
  accent?: string;
  skills: AgentTemplateSkillRef[];
}

/** Full agent template — same as `AgentTemplateSummary` plus the
 *  instructions block. Returned by `GET /api/agent-templates/:slug`. */
export interface AgentTemplate extends AgentTemplateSummary {
  instructions: string;
}

/** Skill reference inside an agent template. `source_url` is the upstream
 *  GitHub / skills.sh URL fetched on create; `cached_*` mirror the upstream
 *  frontmatter at template-author time and let the picker render without
 *  HTTP fetches. */
export interface AgentTemplateSkillRef {
  source_url: string;
  cached_name: string;
  cached_description: string;
}

export interface CreateAgentFromTemplateRequest {
  template_slug: string;
  name: string;
  runtime_id: string;
  model?: string;
  visibility?: AgentVisibility;
  /**
   * Invocation permission mode (MUL-3963). When present it is authoritative;
   * when absent the backend maps the legacy `visibility` field
   * (private -> private, workspace -> public_to + workspace target). On
   * UPDATE, permission changes are OWNER-ONLY (the backend silently ignores
   * these fields from non-owner admins).
   */
  permission_mode?: AgentPermissionMode;
  /** Invocation grants — see `AgentInvocationTargetInput`. */
  invocation_targets?: AgentInvocationTargetInput[];
  max_concurrent_tasks?: number;
  /** Optional overrides applied to the template before creation. nil/omit
   *  uses the template's own value. */
  description?: string;
  instructions?: string;
  avatar_url?: string;
  /** Workspace skill IDs attached **in addition to** the template's
   *  skills. Server dedupes against template skills automatically. */
  extra_skill_ids?: string[];
}

export interface CreateAgentFromTemplateResponse {
  agent: Agent;
  /** Skill IDs that were newly created in the workspace from upstream URLs. */
  imported_skill_ids: string[];
  /** Skill IDs that already existed in the workspace (same name) and were
   *  reused rather than re-imported. The UI can surface this as a toast so
   *  the user knows their pre-existing skill wasn't overwritten. */
  reused_skill_ids: string[];
}

/** 422 body returned by `POST /api/agents/from-template` when one or more
 *  template skill URLs cannot be reached. The transaction is rolled back —
 *  no partial workspace state. */
export interface CreateAgentFromTemplateFailure {
  error: string;
  failed_urls: string[];
}

export interface UpdateAgentRequest {
  name?: string;
  description?: string;
  instructions?: string;
  avatar_url?: string;
  runtime_id?: string;
  runtime_config?: Record<string, unknown>;
  /**
   * NOTE: `custom_env` is intentionally NOT updatable through this
   * request shape. Env edits flow through `client.updateAgentEnv` /
   * `PUT /api/agents/{id}/env` — that path is owner/admin only,
   * denies agent actors, and writes a persistent audit row. The
   * server REJECTS any `PUT /api/agents/{id}` body that includes
   * `custom_env` with a 400; do not put the field in this payload.
   * MUL-2600.
   */
  custom_args?: string[];
  /**
   * MCP server configuration. Tri-state semantics (MUL-2764):
   *   - field omitted → no change
   *   - `null` → clear the column; the daemon falls back to the CLI's
   *     built-in default at launch
   *   - object → replace the stored JSON verbatim; runtime backends
   *     validate / translate it according to their own MCP integration
   */
  mcp_config?: unknown | null;
  /**
   * Composio toolkit allowlist. Tri-state semantics, mirroring the backend
   * gate (MUL-3869):
   *   - field omitted → no change
   *   - `null` → clear the column (no MCP overlay for anyone)
   *   - string[] → wholesale replace; the server lowercases / trims / dedupes
   *     the slugs before persisting
   * Writes are silently dropped server-side unless the caller is the agent
   * owner, so the UI only ever exposes this field through the creator-only
   * MCP tab.
   */
  composio_toolkit_allowlist?: string[] | null;
  visibility?: AgentVisibility;
  /**
   * Invocation permission mode (MUL-3963). When present it is authoritative;
   * when absent the backend maps the legacy `visibility` field
   * (private -> private, workspace -> public_to + workspace target). On
   * UPDATE, permission changes are OWNER-ONLY (the backend silently ignores
   * these fields from non-owner admins).
   */
  permission_mode?: AgentPermissionMode;
  /** Invocation grants — see `AgentInvocationTargetInput`. */
  invocation_targets?: AgentInvocationTargetInput[];
  status?: AgentStatus;
  max_concurrent_tasks?: number;
  model?: string;
  /**
   * Runtime-native reasoning/effort token. Tri-state semantics (MUL-2339):
   *   - field omitted → no change
   *   - "" → clear the override; backend omits the effort flag and the
   *     local CLI config / built-in default decides what the model runs at
   *   - non-empty → set; validated server-side against the target
   *     runtime's provider enum, rejected with 400 if not recognised
   */
  thinking_level?: string;
}

/**
 * Wire shape for the dedicated env-management endpoints
 * (`GET /api/agents/{id}/env` and `PUT /api/agents/{id}/env`). Kept
 * deliberately separate from `Agent` so generic agent reads cannot
 * accidentally surface env values. MUL-2600.
 */
export interface AgentEnvResponse {
  agent_id: string;
  custom_env: Record<string, string>;
}

/**
 * Body for `PUT /api/agents/{id}/env`. Values equal to `"****"` are
 * treated by the server as "preserve the existing value for this key"
 * — a defence-in-depth guard so a UI that round-trips a masked map
 * cannot accidentally clobber real secrets. Submit only the keys
 * touched in the form; omitted keys are removed by the server.
 */
export interface UpdateAgentEnvRequest {
  custom_env: Record<string, string>;
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
	/** Present only when returned from an agent-scoped assignment endpoint. */
	enabled?: boolean;
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

// One (agent, provider, model) row of the "Cost by agent" tab on the runtime
// detail page. provider + model stay on the wire because cost is computed
// client-side from a per-model pricing table (provider disambiguates bare
// model ids that collide across providers) — the client groups these rows by
// agent_id and sums cost per agent across models.
export interface RuntimeUsageByAgent {
  agent_id: string;
  provider: string;
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

// One (date, provider, model) bucket of token usage for the workspace
// dashboard. Workspace-scoped (no runtime_id) and optionally narrowed to a
// single project on the server side. `provider` is kept on the wire so the
// client can disambiguate bare model ids that collide across providers
// (e.g. Cursor's `auto` vs another provider's `auto`) when pricing. Cost
// stays client-side via the model pricing table.
export interface DashboardUsageDaily {
  date: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  task_count: number;
}

// Per-(agent, model) token totals for the workspace dashboard. Identical
// wire shape to RuntimeUsageByAgent — the client folds by agent_id and
// sums cost.
export interface DashboardUsageByAgent {
  agent_id: string;
  provider: string;
  model: string;
  input_tokens: number;
  output_tokens: number;
  cache_read_tokens: number;
  cache_write_tokens: number;
  task_count: number;
}

// Per-agent total terminal-task run-time + counts. Powers the workspace
// dashboard's "time by agent" list. failed_count is a subset of
// task_count (failed tasks still contribute to total_seconds because
// they consumed runtime to fail).
export interface DashboardAgentRunTime {
  agent_id: string;
  total_seconds: number;
  task_count: number;
  failed_count: number;
}

// One (date) bucket of terminal-task run-time + counts for the workspace
// dashboard. Powers the Time and Tasks metrics on the daily-trend toggle
// — same toggle as Tokens / Cost, anchored on completed_at so day buckets
// line up with the per-agent run-time card.
export interface DashboardRunTimeDaily {
  date: string;
  total_seconds: number;
  task_count: number;
  failed_count: number;
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
  /**
   * Per-model reasoning/effort catalog discovered by the daemon. Currently
   * populated for claude, codex, and opencode runtimes; omitted (or undefined)
   * for every other provider, which the UI treats as "no thinking-level
   * picker for this model". See MUL-2339.
   */
  thinking?: RuntimeModelThinking;
}

export interface RuntimeModelThinking {
  /** Levels the user is allowed to pick for this model. */
  supported_levels: RuntimeModelThinkingLevel[];
  /** Informational: the level the upstream CLI documents as its built-in
   *  default when no `--effort` flag is passed. Surfaced by the daemon
   *  but not actively rendered today — Multica's empty `thinking_level`
   *  means "no override; let the local CLI config decide", which may
   *  itself differ from this value. */
  default_level?: string;
}

export interface RuntimeModelThinkingLevel {
  /** Runtime-native token passed to the CLI; never normalised. */
  value: string;
  /** Display label matching each CLI's own UI (`Low`, `Extra high`, …). */
  label: string;
  /** Optional helper copy lifted from upstream catalog
   *  (`codex debug models` emits one per level). */
  description?: string;
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
  | "conflict"
  | "failed"
  | "timeout";

export type RuntimeLocalSkillImportAction = "overwrite";

export interface RuntimeLocalSkillImportConflict {
  existing_skill_id: string;
  existing_created_by?: string;
  can_overwrite: boolean;
}

export interface RuntimeLocalSkillSummary {
  key: string;
  name: string;
  description?: string;
  source_path: string;
  provider: string;
  /**
   * Which discovery root surfaced this skill: "provider" for the runtime's
   * own skill directory (e.g. ~/.claude/skills) or "universal" for the
   * cross-tool ~/.agents/skills fallback. Daemons that predate multi-root
   * discovery omit the field; treat `undefined` as unknown rather than
   * asserting either origin.
   */
  root?: "provider" | "universal" | "plugin";
  /** Enabled runtime plugin that contributed this skill, when applicable. */
  plugin?: string;
  /** New daemons set this only when they can enforce per-agent disablement. */
  can_disable?: boolean;
  file_count: number;
}

export interface RuntimeLocalMcpServerSummary {
	name: string;
	transport?: "stdio" | "http" | "sse" | "unknown";
	source?: string;
	enabled: boolean;
}

export interface RuntimeLocalSkillListRequest {
  id: string;
  runtime_id: string;
  status: RuntimeLocalSkillStatus;
  skills?: RuntimeLocalSkillSummary[];
  supported: boolean;
	mcp_servers?: RuntimeLocalMcpServerSummary[];
	mcp_supported?: boolean;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface CreateRuntimeLocalSkillImportRequest {
  skill_key: string;
  name?: string;
  description?: string;
  action?: RuntimeLocalSkillImportAction;
  target_skill_id?: string;
  supports_conflict?: boolean;
}

export interface RuntimeLocalSkillImportRequest {
  id: string;
  runtime_id: string;
  skill_key: string;
  name?: string;
  description?: string;
  action?: RuntimeLocalSkillImportAction;
  target_skill_id?: string;
  supports_conflict?: boolean;
  status: RuntimeLocalSkillStatus;
  skill?: Skill;
  conflict?: RuntimeLocalSkillImportConflict;
  error?: string;
  created_at: string;
  updated_at: string;
}

export interface RuntimeLocalSkillsResult {
  skills: RuntimeLocalSkillSummary[];
  supported: boolean;
	mcpServers: RuntimeLocalMcpServerSummary[];
	mcpSupported: boolean;
}

export interface RuntimeLocalSkillImportResult {
  status: "created" | "updated" | "conflict";
  skill?: Skill;
  conflict?: RuntimeLocalSkillImportConflict;
}
