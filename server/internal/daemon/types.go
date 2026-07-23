package daemon

import (
	"encoding/json"

	"github.com/multica-ai/multica/server/internal/runtimeapps"
)

// AgentEntry describes a single available agent CLI.
type AgentEntry struct {
	Path string // path to CLI binary (pinned at startup; symlink-resolved to a concrete, possibly versioned, path)
	// Command is the bare command name or MULTICA_*_PATH value that Path was
	// resolved from at startup. It is kept so the daemon can re-resolve Path
	// if the pinned executable later vanishes — e.g. a version manager
	// (Homebrew Cask, nvm/fnm) does an in-place upgrade that deletes the old
	// versioned directory Path points into. Empty for synthesized entries
	// (custom runtime profiles) that carry an absolute path directly. See
	// Daemon.resolveAgentEntry and MUL-4486.
	Command string
	Model   string // model override (optional)
}

// Runtime represents a registered daemon runtime.
type Runtime struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Provider string `json:"provider"`
	Status   string `json:"status"`
	// ProfileID is non-empty when this runtime was registered from a
	// workspace custom runtime profile (MUL-3284). It links the runtime row
	// back to the profile so the daemon can resolve the profile's
	// command_name to the executable to launch. Built-in (provider-detected)
	// runtimes leave this empty.
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

// ConnectedAppData keeps the claim-response field local to daemon types while
// sharing the canonical JSON shape with the runtime app metadata package.
type ConnectedAppData = runtimeapps.ConnectedApp

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
	WorkspaceContext         string                 `json:"workspace_context,omitempty"`
	ThreadName               string                 `json:"thread_name,omitempty"` // semantic title for provider-native session/thread history
	Agent                    *AgentData             `json:"agent,omitempty"`
	ConnectedApps            []ConnectedAppData     `json:"connected_apps,omitempty"` // per-run app capabilities mounted through runtime MCP overlays
	Repos                    []RepoData             `json:"repos,omitempty"`
	ProjectID                string                 `json:"project_id,omitempty"`                  // issue's project, when present
	ProjectTitle             string                 `json:"project_title,omitempty"`               // human-readable project title for context injection
	ProjectDescription       string                 `json:"project_description,omitempty"`         // durable project-level context injected into the brief
	ProjectResources         []ProjectResourceData  `json:"project_resources,omitempty"`           // project-scoped resources to expose to the agent
	IsLeaderTask             bool                   `json:"is_leader_task,omitempty"`              // true when executing in the squad-leader coordinator role
	PriorSessionID           string                 `json:"prior_session_id,omitempty"`            // Claude session ID from a previous task on this issue
	PriorWorkDir             string                 `json:"prior_work_dir,omitempty"`              // work_dir from a previous task on this issue
	TriggerCommentID         string                 `json:"trigger_comment_id,omitempty"`          // comment that triggered this task
	CoalescedCommentIDs      []string               `json:"coalesced_comment_ids,omitempty"`       // MUL-4195: earlier comments folded into this run while it was still queued; the agent must address these in addition to the (newest) triggering comment. Empty for old servers / non-merged runs
	CoalescedComments        []CoalescedCommentData `json:"coalesced_comments,omitempty"`          // MUL-4195: full detail of the folded comments (thread_id/author/created_at/content) so the prompt can address each without assuming a shared thread. Empty for old servers / non-merged runs
	TriggerThreadID          string                 `json:"trigger_thread_id,omitempty"`           // root comment ID for the triggering thread; falls back to trigger_comment_id on old servers
	TriggerCommentContent    string                 `json:"trigger_comment_content,omitempty"`     // content of the triggering comment
	TriggerAuthorType        string                 `json:"trigger_author_type,omitempty"`         // "agent" or "member" — author kind for the triggering comment
	TriggerAuthorName        string                 `json:"trigger_author_name,omitempty"`         // display name of the triggering comment author
	NewCommentCount          int                    `json:"new_comment_count,omitempty"`           // issue-wide comments since this agent's last run (excludes its own and the injected trigger); 0/omitted for old daemons or cold start
	NewCommentsSince         string                 `json:"new_comments_since,omitempty"`          // RFC3339 anchor (last run's started_at) the count is measured from; empty on cold start
	ChatSessionID            string                 `json:"chat_session_id,omitempty"`             // non-empty for chat tasks
	ChatChannelType          string                 `json:"chat_channel_type,omitempty"`           // "slack" when the chat session is backed by an IM channel; empty for a web-only chat. Drives the channel-awareness block in the prompt
	ChatInThread             bool                   `json:"chat_in_thread,omitempty"`              // true when the latest @mention was a thread reply; selects which read command the prompt tells the agent to start with
	ChatMessage              string                 `json:"chat_message,omitempty"`                // user message content for chat tasks
	ChatMessageAttachments   []ChatAttachmentMeta   `json:"chat_message_attachments,omitempty"`    // attachments linked to the chat message; agent uses these to `multica attachment download <id>`
	ChatIntro                bool                   `json:"chat_intro,omitempty"`                  // true for the agent's proactive self-introduction chat (no user message); selects the self-introduction prompt in buildChatPrompt
	AutopilotRunID           string                 `json:"autopilot_run_id,omitempty"`            // non-empty for autopilot run_only tasks
	AutopilotID              string                 `json:"autopilot_id,omitempty"`                // autopilot that spawned this run
	AutopilotTitle           string                 `json:"autopilot_title,omitempty"`             // autopilot title used as task context
	AutopilotDescription     string                 `json:"autopilot_description,omitempty"`       // autopilot description used as task prompt
	AutopilotSource          string                 `json:"autopilot_source,omitempty"`            // manual, schedule, webhook, or api
	AutopilotTriggerPayload  json.RawMessage        `json:"autopilot_trigger_payload,omitempty"`   // optional trigger payload for webhook/api runs
	QuickCreatePrompt        string                 `json:"quick_create_prompt,omitempty"`         // user's natural-language input for quick-create tasks
	QuickCreatePriority      string                 `json:"quick_create_priority,omitempty"`       // explicit priority selected in quick-create
	QuickCreateDueDate       string                 `json:"quick_create_due_date,omitempty"`       // explicit calendar due date selected in quick-create
	QuickCreateAttachmentIDs []string               `json:"quick_create_attachment_ids,omitempty"` // attachments uploaded in the quick-create prompt and bound by issue create
	HandoffNote              string                 `json:"handoff_note,omitempty"`                // assignment handoff instruction; rendered into the opening prompt + issue_context.md

	SquadID               string `json:"squad_id,omitempty"`                // when the picker was a squad, the squad's UUID; Agent is still the resolved leader
	SquadName             string `json:"squad_name,omitempty"`              // display name for the picker squad, used in prompt text
	ParentIssueID         string `json:"parent_issue_id,omitempty"`         // for quick-create tasks opened from "Add sub issue" — UUID of the parent issue the new issue should be filed under
	ParentIssueIdentifier string `json:"parent_issue_identifier,omitempty"` // human-readable identifier (e.g. MUL-123) of the quick-create parent issue, used in prompt context
	// RequestingUserName + RequestingUserProfileDescription describe the human
	// the agent is working on behalf of. v1 sources them from the runtime
	// owner (the user who registered the daemon). Empty when the runtime has
	// no owner (cloud / system runtimes) or the user hasn't set a description.
	// Injected into the brief under `## Requesting User`; omitted entirely
	// when description is empty so the agent doesn't see a useless heading.
	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`
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
	// Empty or non-task-scoped values are fatal for writable agent tasks; the
	// daemon must not fall back to its own token. See MUL-3292.
	AuthToken string `json:"auth_token,omitempty"`
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

// CoalescedCommentData mirrors the server-side struct (handler.CoalescedCommentData):
// the full detail of a comment folded into this run while it was still queued
// (MUL-4195). The prompt embeds each one directly so the agent addresses every
// folded comment without assuming they all live in the triggering thread.
type CoalescedCommentData struct {
	ID         string `json:"id"`
	ThreadID   string `json:"thread_id,omitempty"`
	AuthorType string `json:"author_type,omitempty"`
	AuthorName string `json:"author_name,omitempty"`
	Content    string `json:"content"`
	CreatedAt  string `json:"created_at,omitempty"`
}

// AgentData holds agent details returned by the claim endpoint.
type AgentData struct {
	ID                    string                     `json:"id"`
	Name                  string                     `json:"name"`
	Instructions          string                     `json:"instructions"`
	Skills                []SkillData                `json:"skills,omitempty"`
	SkillRefs             []SkillRefData             `json:"skill_refs,omitempty"`
	CustomEnv             map[string]string          `json:"custom_env,omitempty"`
	CustomArgs            []string                   `json:"custom_args,omitempty"`
	McpConfig             json.RawMessage            `json:"mcp_config,omitempty"`
	Model                 string                     `json:"model,omitempty"`
	ThinkingLevel         string                     `json:"thinking_level,omitempty"`
	ServiceTier           string                     `json:"service_tier,omitempty"`
	DisabledRuntimeSkills []DisabledRuntimeSkillData `json:"disabled_runtime_skills,omitempty"`
	// RuntimeConfig is the per-provider runtime_config JSON as stored on
	// the agent record, forwarded verbatim by the claim endpoint. The
	// daemon decodes provider-specific fields (e.g. openclaw mode +
	// gateway endpoint, see issue #3260); other backends ignore it.
	RuntimeConfig json.RawMessage `json:"runtime_config,omitempty"`
}

// DisabledRuntimeSkillData is the task-wire identity of one runtime-local
// skill that must be hidden from this agent's provider process.
type DisabledRuntimeSkillData struct {
	RuntimeID string `json:"runtime_id"`
	Provider  string `json:"provider"`
	Root      string `json:"root"`
	Key       string `json:"key"`
	Name      string `json:"name,omitempty"`
	Plugin    string `json:"plugin,omitempty"`
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
