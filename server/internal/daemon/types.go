package daemon

// CEREBRO-PATCH(daemon-types): cerebro modification of upstream file

import "encoding/json"

// AgentEntry describes a single available agent CLI.
type AgentEntry struct {
	Path  string // path to CLI binary
	Model string // model override (optional)
}

// Runtime represents a registered daemon runtime.
type Runtime struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
}

// RepoData holds repository information from the workspace.
type RepoData struct {
	URL string `json:"url"`
}

// ProjectResourceData mirrors handler.ProjectResourceData — a single project
// resource as delivered to the daemon. resource_ref is type-specific JSON.
type ProjectResourceData struct {
	ID           string          `json:"id"`
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
	Label        string          `json:"label,omitempty"`
}

type ChatHistoryMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Task represents a claimed task from the server.
// Agent data (name, skills) is populated by the claim endpoint.
type Task struct {
	ID                    string                `json:"id"`
	AgentID               string                `json:"agent_id"`
	RuntimeID             string                `json:"runtime_id"`
	IssueID               string                `json:"issue_id"`
	WorkspaceID           string                `json:"workspace_id"`
	Agent                 *AgentData            `json:"agent,omitempty"`
	Repos                 []RepoData            `json:"repos,omitempty"`
	ProjectID             string                `json:"project_id,omitempty"`              // issue's project, when present
	ProjectTitle          string                `json:"project_title,omitempty"`           // human-readable project title for context injection
	ProjectResources      []ProjectResourceData `json:"project_resources,omitempty"`       // project-scoped resources to expose to the agent
	PriorSessionID        string                `json:"prior_session_id,omitempty"`        // Claude session ID from a previous task on this issue
	PriorWorkDir          string                `json:"prior_work_dir,omitempty"`          // work_dir from a previous task on this issue
	TriggerCommentID      string                `json:"trigger_comment_id,omitempty"`      // comment that triggered this task
	TriggerCommentContent string                `json:"trigger_comment_content,omitempty"` // content of the triggering comment
	TriggerAuthorType     string                `json:"trigger_author_type,omitempty"`     // "agent" or "member" — author kind for the triggering comment
	TriggerAuthorName     string                `json:"trigger_author_name,omitempty"`     // display name of the triggering comment author
	ChatSessionID         string                `json:"chat_session_id,omitempty"`         // non-empty for chat tasks
	// CEREBRO-PATCH(chat-message-id-claim): JEH-1083 — pre-created assistant chat_message UUID exposed to the agent as MULTICA_CHAT_MESSAGE_ID so the MCP add_attachment tool can link files to the in-flight chat reply.
	ChatMessageID string `json:"chat_message_id,omitempty"`
	// CEREBRO-PATCH(daemon-task-chat-messages): cerebro accumulates a list of
	// user messages newer than the last assistant reply (oldest first) so the
	// daemon can build a prompt covering bursts; ChatMessage stays for
	// backwards compat with pre-JEH-330 daemons.
	ChatHistory             []ChatHistoryMessage `json:"chat_history,omitempty"`              // capped chat transcript for stateless managed HTTP runtimes
	ChatMessages            []string             `json:"chat_messages,omitempty"`             // user messages newer than the last assistant reply (oldest first)
	ChatMessage             string               `json:"chat_message,omitempty"`              // user message content for chat tasks
	AutopilotRunID          string               `json:"autopilot_run_id,omitempty"`          // non-empty for autopilot run_only tasks
	AutopilotID             string               `json:"autopilot_id,omitempty"`              // autopilot that spawned this run
	AutopilotTitle          string               `json:"autopilot_title,omitempty"`           // autopilot title used as task context
	AutopilotDescription    string               `json:"autopilot_description,omitempty"`     // autopilot description used as task prompt
	AutopilotSource         string               `json:"autopilot_source,omitempty"`          // manual, schedule, webhook, or api
	AutopilotTriggerPayload json.RawMessage      `json:"autopilot_trigger_payload,omitempty"` // optional trigger payload for webhook/api runs
	QuickCreatePrompt       string               `json:"quick_create_prompt,omitempty"`       // user's natural-language input for quick-create tasks
	// CEREBRO-PATCH(daemon-task-user-profile-prompt): compiled per-user
	// communication prompt (JEH-304).
	UserProfilePrompt string `json:"user_profile_prompt,omitempty"`
	// CEREBRO-PATCH(daemon-task-token): TaskToken is a short-lived (~1h),
	// scope-limited token (mtt_) the daemon must inject as MULTICA_TOKEN
	// for the spawned agent process. Falling back to the daemon's own PAT
	// would defeat the security model — agents must NEVER see the daemon's
	// full token. JEH-324.
	TaskToken string `json:"task_token,omitempty"`
	// CEREBRO-PATCH(daemon-task-sandbox-enabled): per-runtime sandbox
	// override (JEH-418). nil means "no override, fall back to the daemon's
	// MULTICA_ENABLE_SANDBOX default"; true/false force sandbox on/off for
	// this task regardless of the env var. Carried on every claim so an
	// admin's UI toggle takes effect at the next claim — no daemon restart
	// needed.
	SandboxEnabled *bool `json:"sandbox_enabled,omitempty"`
	// CEREBRO-PATCH(daemon-task-persona-spawn): JEH-1080 — spawning user + group memberships piped to the persona-hook as facts.
	PersonaSpawnUserID   string   `json:"persona_spawn_user_id,omitempty"`
	PersonaSpawnGroupIDs []string `json:"persona_spawn_group_ids,omitempty"`
	// RuntimePersonaSandbox is the runtime-level persona sandbox upper
// CEREBRO-PATCH(types): persona integration additions.
	// bound (E1). Empty = no upper bound; the agent's PersonaSandbox
	// decides alone. Non-empty = preparePersonaSpawn must use this name
	// and ignore the agent-level value so an operator's runtime-wide cap
	// can't be bypassed by an agent owner picking a permissive sandbox.
	RuntimePersonaSandbox string `json:"runtime_persona_sandbox,omitempty"`
	// CEREBRO-PATCH(daemon-task-model-override): per-task model override
	// from agent_task_queue.model_override (JEH-1310). Empty = fall back to
	// Agent.Model, then env, then CLI default — see daemon.go model-resolution.
	ModelOverride string `json:"model_override,omitempty"`
}

// AgentData holds agent details returned by the claim endpoint.
type AgentData struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Instructions string            `json:"instructions"`
	Skills       []SkillData       `json:"skills"`
	CustomEnv    map[string]string `json:"custom_env,omitempty"`
	CustomArgs   []string          `json:"custom_args,omitempty"`
	McpConfig    json.RawMessage   `json:"mcp_config,omitempty"`
	Model        string            `json:"model,omitempty"`
	// CEREBRO-PATCH(daemon-agent-sandbox-allowlist): admin-set list of
	// additional outbound hosts (host:port) that this specific agent is
	// allowed to reach when the macOS sandbox is enabled. Merged on top of
	// the daemon-wide allowlist.
	SandboxAllowlist []string `json:"sandbox_allowlist,omitempty"`
	// PersonaSandbox is the name of the persona sandbox to gate this
	// agent's tool calls (e.g. "claude-developer"). Empty = no persona
	// gating; the daemon falls back to its existing behaviour.
	PersonaSandbox string `json:"persona_sandbox,omitempty"`
}

// SkillData represents a structured skill for task execution.
type SkillData struct {
	Name    string          `json:"name"`
	Content string          `json:"content"`
	Files   []SkillFileData `json:"files,omitempty"`
}

// SkillFileData represents a supporting file within a skill.
type SkillFileData struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// TaskUsageEntry represents token usage for a single model during a task execution.
type TaskUsageEntry struct {
	Provider         string `json:"provider"`
	Model            string `json:"model"`
	InputTokens      int64  `json:"input_tokens"`
	OutputTokens     int64  `json:"output_tokens"`
	CacheReadTokens  int64  `json:"cache_read_tokens"`
	CacheWriteTokens int64  `json:"cache_write_tokens"`
	// CEREBRO-PATCH(daemon-types-firtal-gateway-usage-cost): forward exact gateway spend (always emit, matches TaskUsagePayload shape).
	CostCents int64 `json:"cost_cents"`
}

// TaskResult is the outcome of executing a task.
type TaskResult struct {
	Status        string           `json:"status"`
	Comment       string           `json:"comment"`
	BranchName    string           `json:"branch_name,omitempty"`
	EnvType       string           `json:"env_type,omitempty"`
	SessionID     string           `json:"session_id,omitempty"` // Claude session ID for future resumption
	WorkDir       string           `json:"work_dir,omitempty"`   // working directory used during execution
	EnvRoot       string           `json:"-"`                    // env root dir for writing GC metadata (not sent to server)
	FailureReason string           `json:"-"`                    // classifier forwarded to FailTask on the blocked path; empty falls back to 'agent_error'
	Usage         []TaskUsageEntry `json:"usage,omitempty"`      // per-model token usage
}
