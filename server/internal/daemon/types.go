package daemon

// CEREBRO-PATCH(daemon-types): cerebro modification of upstream file

import "encoding/json"

// AgentEntry describes a single available agent CLI.
type AgentEntry struct {
	Path        string // path to CLI binary
	Model       string // model override (optional)
	DisplayName string // CEREBRO-PATCH(daemon-firtal-gateway-runtime-name): optional display name override sent to server
}

// Runtime represents a registered daemon runtime.
type Runtime struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Provider  string `json:"provider"`
	Status    string `json:"status"`
	ProfileID string `json:"profile_id,omitempty"`
}

// RepoData holds repository information from the workspace.
type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description,omitempty"`
	Ref         string `json:"ref,omitempty"`
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
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	RuntimeID   string `json:"runtime_id"`
	IssueID     string `json:"issue_id"`
	WorkspaceID string `json:"workspace_id"`
	// WorkspaceContext mirrors workspace.context (the per-workspace system
	// prompt set in Settings → General). Server populates this on every claim
	// regardless of task kind so the daemon can inject `## Workspace Context`
	// into the brief. Empty when the owner hasn't set one.

	WorkspaceContext        string                `json:"workspace_context,omitempty"`
	ThreadName              string                `json:"thread_name,omitempty"` // semantic title for provider-native session/thread history
	Agent                   *AgentData            `json:"agent,omitempty"`
	Repos                   []RepoData            `json:"repos,omitempty"`
	ProjectID               string                `json:"project_id,omitempty"`                 // issue's project, when present
	ProjectTitle            string                `json:"project_title,omitempty"`              // human-readable project title for context injection
	ProjectDescription      string                `json:"project_description,omitempty"`        // durable project-level context injected into the brief
	ProjectResources        []ProjectResourceData `json:"project_resources,omitempty"`          // project-scoped resources to expose to the agent
	PriorSessionID          string                `json:"prior_session_id,omitempty"`           // Claude session ID from a previous task on this issue
	PriorWorkDir            string                `json:"prior_work_dir,omitempty"`             // work_dir from a previous task on this issue
	TriggerCommentID        string                `json:"trigger_comment_id,omitempty"`         // comment that triggered this task
	TriggerThreadID         string                `json:"trigger_thread_id,omitempty"`          // root comment ID for the triggering thread; falls back to trigger_comment_id on old servers
	TriggerCommentContent   string                `json:"trigger_comment_content,omitempty"`    // content of the triggering comment
	TriggerCommentCreatedAt string                `json:"trigger_comment_created_at,omitempty"` // RFC3339 timestamp for the triggering comment
	HandoffNote             string                `json:"handoff_note,omitempty"`
	TriggerAuthorType       string                `json:"trigger_author_type,omitempty"` // "agent" or "member" — author kind for the triggering comment
	TriggerAuthorName       string                `json:"trigger_author_name,omitempty"` // display name of the triggering comment author
	// CEREBRO-PATCH(wakeup-system-activity): daemon receives wakeup context separately from comments.
	WakeupPrompt             string               `json:"wakeup_prompt,omitempty"`               // prompt stored on a platform wakeup task
	WakeupTriggerType        string               `json:"wakeup_trigger_type,omitempty"`         // time, issue_status, or github_ci for wakeup tasks
	NewCommentCount          int                  `json:"new_comment_count,omitempty"`           // issue-wide comments since this agent's last run (excludes its own and the injected trigger); 0/omitted for old daemons or cold start
	NewCommentsSince         string               `json:"new_comments_since,omitempty"`          // RFC3339 anchor (last run's started_at) the count is measured from; empty on cold start
	ChatSessionID            string               `json:"chat_session_id,omitempty"`             // non-empty for chat tasks
	ChatMessage              string               `json:"chat_message,omitempty"`                // user message content for chat tasks
	ChatMessageAttachments   []ChatAttachmentMeta `json:"chat_message_attachments,omitempty"`    // attachments linked to the chat message; agent uses these to `multica attachment download <id>`
	AutopilotRunID           string               `json:"autopilot_run_id,omitempty"`            // non-empty for autopilot run_only tasks
	AutopilotID              string               `json:"autopilot_id,omitempty"`                // autopilot that spawned this run
	AutopilotTitle           string               `json:"autopilot_title,omitempty"`             // autopilot title used as task context
	AutopilotDescription     string               `json:"autopilot_description,omitempty"`       // autopilot description used as task prompt
	AutopilotSource          string               `json:"autopilot_source,omitempty"`            // manual, schedule, webhook, or api
	AutopilotTriggerPayload  json.RawMessage      `json:"autopilot_trigger_payload,omitempty"`   // optional trigger payload for webhook/api runs
	QuickCreatePrompt        string               `json:"quick_create_prompt,omitempty"`         // user's natural-language input for quick-create tasks
	QuickCreateAttachmentIDs []string             `json:"quick_create_attachment_ids,omitempty"` // attachments uploaded in the quick-create prompt and bound by issue create
	SquadID                  string               `json:"squad_id,omitempty"`                    // when the picker was a squad, the squad's UUID; Agent is still the resolved leader
	SquadName                string               `json:"squad_name,omitempty"`                  // display name for the picker squad, used in prompt text
	ParentIssueID            string               `json:"parent_issue_id,omitempty"`             // for quick-create tasks opened from "Add sub issue" — UUID of the parent issue the new issue should be filed under
	ParentIssueIdentifier    string               `json:"parent_issue_identifier,omitempty"`     // human-readable identifier (e.g. MUL-123) of the quick-create parent issue, used in prompt context
	// RequestingUserName + RequestingUserProfileDescription describe the human
	// the agent is working on behalf of. v1 sources them from the runtime
	// owner (the user who registered the daemon). Empty when the runtime has
	// no owner (cloud / system runtimes) or the user hasn't set a description.
	// Injected into the brief under `## Requesting User`; omitted entirely
	// when description is empty so the agent doesn't see a useless heading.
	RequestingUserName               string               `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string               `json:"requesting_user_profile_description,omitempty"`
	UserProfilePrompt                string               `json:"user_profile_prompt,omitempty"`
	Kind                             string               `json:"kind,omitempty"`
	Title                            string               `json:"title,omitempty"`
	ModelOverride                    string               `json:"model_override,omitempty"` // CEREBRO-PATCH(daemon-task-model-override): per-task model override (JEH-1310).
	SandboxEnabled                   *bool                `json:"sandbox_enabled,omitempty"`
	RuntimeSandboxPolicy             json.RawMessage      `json:"runtime_sandbox_policy,omitempty"`
	RuntimePersonaSandbox            string               `json:"runtime_persona_sandbox,omitempty"`
	RuntimeToolsConfig               json.RawMessage      `json:"runtime_tools_config,omitempty"`
	DisallowedMCPTools               []string             `json:"disallowed_mcp_tools,omitempty"` // CEREBRO-PATCH(daemon-task-disallowed-mcp-tools): TECH-3156 per-tool connection denies.
	EffectiveTools                   []TaskToolBriefEntry `json:"effective_tools,omitempty"`      // CEREBRO-PATCH(daemon-task-effective-tools): FIR-2312 non-CLI tools (MCP + connections) the agent may use, rendered into the brief.
	PersonaSpawnUserID               string               `json:"persona_spawn_user_id,omitempty"`
	PersonaSpawnGroupIDs             []string             `json:"persona_spawn_group_ids,omitempty"`
	ChatHistory                      []ChatHistoryMessage `json:"chat_history,omitempty"`
	ChatMessages                     []string             `json:"chat_messages,omitempty"`
	ChatMessageID                    string               `json:"chat_message_id,omitempty"`
	// Initiator* identify the actor who triggered THIS task (the real
	// requester behind the current comment/mention or chat message) as
	// distinct from the runtime owner whose credentials the agent runs with.
	// Comment-triggered tasks resolve to the triggering comment's author;
	// chat tasks resolve to the chat session creator. Empty for task kinds
	// with no attributable human initiator (on-assign, autopilot,
	// quick-create). InitiatorEmail is set only for member initiators. The
	// daemon emits these into the brief under `## Task Initiator` so a
	// workspace-visible agent can attribute the request per person. The
	// agent's effective credentials stay owner-scoped — this is an attested
	// identity, not a credential. See MUL-2645.
	InitiatorType  string `json:"initiator_type,omitempty"`
	InitiatorID    string `json:"initiator_id,omitempty"`
	InitiatorName  string `json:"initiator_name,omitempty"`
	InitiatorEmail string `json:"initiator_email,omitempty"`
	// AuthToken is the task-scoped credential the server mints at claim time.
	// The daemon injects it into the spawned agent as MULTICA_TOKEN so the
	// agent never sees the daemon's own (often workspace-owner) credential.
	// Empty when the server-side runtime has no owning user — the daemon
	// then falls back to its own token. See MUL-2600. Replaces the fork's
	// own TaskToken field (JEH-324), now superseded by upstream's model.
	AuthToken string `json:"auth_token,omitempty"`
	// CEREBRO-PATCH(chat-message-id-claim): JEH-1083 — pre-created assistant chat_message UUID exposed to the agent as MULTICA_CHAT_MESSAGE_ID so the MCP add_attachment tool can link files to the in-flight chat reply.
	// CEREBRO-PATCH(daemon-task-chat-messages): cerebro accumulates a list of
	// CEREBRO-PATCH(daemon-task-user-profile-prompt): compiled per-user
	// CEREBRO-PATCH(daemon-task-sandbox-enabled): per-runtime sandbox
	// CEREBRO-PATCH(daemon-task-persona-spawn): JEH-1080 — spawning user + group memberships piped to the persona-hook as facts.
	// CEREBRO-PATCH(types): persona integration additions.
	// CEREBRO-PATCH(daemon-task-model-override): per-task model override
	// CEREBRO-PATCH(daemon-task-runtime-tools-config): runtime-level MCP defaults JSON (9031). Merged with Agent.McpConfig in runTask.

	// CEREBRO-PATCH(daemon-trace-upload-trigger-user): FIR-2438 — user who triggered the run.
	TriggerUserID string `json:"trigger_user_id,omitempty"`
	// CEREBRO-PATCH(daemon-trace-upload-trigger-user-name): TECH-3295 R3 — display name of the chain's human origin, used for the trace user-label instead of the handoff agent's name.
	TriggerUserName string `json:"trigger_user_name,omitempty"`
	// CEREBRO-PATCH(daemon-trace-upload-display-titles): FIR-2763 M1 display titles resolved at claim time; empty when unknown.
	IssueTitle       string `json:"issue_title,omitempty"`
	ParentIssueTitle string `json:"parent_issue_title,omitempty"`
	// CEREBRO-PATCH(daemon-trace-upload-channel-label): FIR-2438 — issue kind for surface classification.
	IssueKind string `json:"issue_kind,omitempty"`
	// CEREBRO-PATCH(daemon-bundled-prompt): FIR-2384 step 2 — hint set by server when workspace has bundled-read saving on.
	BundleContextHint bool `json:"bundle_context_hint,omitempty"`
	// CEREBRO-PATCH(daemon-snapshot-prompt): FIR-2384 — pre-rendered issue+thread snapshot shipped at claim time.
	IssueSnapshot string `json:"issue_snapshot,omitempty"`
	// CEREBRO-PATCH(daemon-graphify-nudge): FIR-1311 — standing "use the graphify code graph" instruction shipped at claim time when the graphify saving is on.
	GraphifyNudge string `json:"graphify_nudge,omitempty"`
	// CEREBRO-PATCH(daemon-memory-autorecall): FIR-1794 layer 3 — automatically recalled memories shipped at claim time when cerebro_memory is on.
	MemoryContext string `json:"memory_context,omitempty"`
	// CEREBRO-PATCH(daemon-task-presentation-mode): receive runtime
	// presentation_mode from claim response. "interactive" tells the daemon
	// to mirror agent stdout to the cerebro terminal broker so a browser
	// can watch the run live. Empty / "headless" = no mirroring.
	PresentationMode string `json:"presentation_mode,omitempty"`
	// CEREBRO-PATCH(daemon-tool-policy-ipc): TECH-2563 — staged-rollout mode for
	// local-runtime per-tool enforcement ("off"|"observe"|"enforce"), resolved
	// server-side from workspace settings at claim. Empty/"off" = no PreToolUse
	// hook wired (default, no behaviour change); observe/enforce wires the hook.
	LocalToolPolicyStage string `json:"local_tool_policy_stage,omitempty"`
}

// ChatAttachmentMeta is the structured attachment metadata the daemon
// hands to the agent for chat tasks. We pass id + filename + content_type
// so the chat prompt can list them explicitly and instruct the agent to
// run `multica attachment download <id>` instead of guessing from a
// signed CDN URL (which expires).
type ChatAttachmentMeta struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	ContentType string `json:"content_type,omitempty"`
}

// AgentData holds agent details returned by the claim endpoint.
type AgentData struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Instructions string            `json:"instructions"`
	Skills       []SkillData       `json:"skills"`
	SkillRefs    []SkillRefData    `json:"skill_refs,omitempty"`
	CustomEnv    map[string]string `json:"custom_env,omitempty"`
	CustomArgs   []string          `json:"custom_args,omitempty"`
	McpConfig    json.RawMessage   `json:"mcp_config,omitempty"`
	// CEREBRO-PATCH(agent-infisical-secrets): resolved secrets the backend
	// fetched via the per-user scoped Infisical machine identity, ready to
	// inject as env at spawn. The daemon no longer talks to Infisical —
	// secrets arrive over the already-TLS-protected claim path. FIR-2192.
	InfisicalSecrets map[string]string `json:"infisical_secrets,omitempty"`
	Model            string            `json:"model,omitempty"`
	ThinkingLevel    string            `json:"thinking_level,omitempty"`
	// CEREBRO-PATCH(daemon-agent-sandbox-allowlist): admin-set list of
	SandboxAllowlist      []string        `json:"sandbox_allowlist,omitempty"`
	PersonaSandbox        string          `json:"persona_sandbox,omitempty"`
	RuntimePersonaSandbox string          `json:"runtime_persona_sandbox,omitempty"`
	RuntimeConfig         json.RawMessage `json:"runtime_config,omitempty"`
}

// SkillData represents a structured skill for task execution.
type SkillData struct {
	ID          string          `json:"id"`
	Source      string          `json:"source,omitempty"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Hash        string          `json:"hash,omitempty"`
	SizeBytes   int64           `json:"size_bytes,omitempty"`
	Content     string          `json:"content"`
	Files       []SkillFileData `json:"files,omitempty"`
}

// SkillFileData represents a supporting file within a skill.
type SkillFileData struct {
	Path      string `json:"path"`
	Content   string `json:"content"`
	SHA256    string `json:"sha256,omitempty"`
	SizeBytes int64  `json:"size_bytes,omitempty"`
}

type SkillRefData struct {
	ID          string             `json:"id"`
	Source      string             `json:"source"`
	Name        string             `json:"name"`
	Description string             `json:"description,omitempty"`
	Hash        string             `json:"hash"`
	SizeBytes   int64              `json:"size_bytes"`
	FileCount   int                `json:"file_count"`
	Files       []SkillFileRefData `json:"files,omitempty"`
}

type SkillFileRefData struct {
	Path      string `json:"path"`
	SHA256    string `json:"sha256"`
	SizeBytes int64  `json:"size_bytes"`
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
	// CEREBRO-PATCH(daemon-types-context-footprint): FIR-1856 forward the last-turn context footprint for the window indicator (omitempty: only Codex sets it).
	ContextInputTokens     int64 `json:"context_input_tokens,omitempty"`
	ContextCacheReadTokens int64 `json:"context_cache_read_tokens,omitempty"`
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
	// CEREBRO-PATCH(daemon-task-result-logs): JEH-1365 — verbose log content
	// accumulated during the run (not sent to server; used for quota-signal parsing).
	Logs string `json:"-"`
}
