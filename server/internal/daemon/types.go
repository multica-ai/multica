package daemon

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

// Task represents a claimed task from the server.
// Agent data (name, skills) is populated by the claim endpoint.
type Task struct {
	ID          string `json:"id"`
	AgentID     string `json:"agent_id"`
	RuntimeID   string `json:"runtime_id"`
	IssueID     string `json:"issue_id"`
	WorkspaceID string `json:"workspace_id"`
	WorkspaceContext         string                `json:"workspace_context,omitempty"`
	ThreadName               string                `json:"thread_name,omitempty"`
	Agent                    *AgentData            `json:"agent,omitempty"`
	Repos                    []RepoData            `json:"repos,omitempty"`
	ProjectID                string                `json:"project_id,omitempty"`
	ProjectTitle             string                `json:"project_title,omitempty"`
	ProjectDescription       string                `json:"project_description,omitempty"`
	ProjectResources         []ProjectResourceData `json:"project_resources,omitempty"`
	PriorSessionID           string                `json:"prior_session_id,omitempty"`
	PriorWorkDir             string                `json:"prior_work_dir,omitempty"`
	TriggerCommentID         string                `json:"trigger_comment_id,omitempty"`
	TriggerThreadID          string                `json:"trigger_thread_id,omitempty"`
	TriggerCommentContent    string                `json:"trigger_comment_content,omitempty"`
	TriggerAuthorType        string                `json:"trigger_author_type,omitempty"`
	TriggerAuthorName        string                `json:"trigger_author_name,omitempty"`
	NewCommentCount          int                   `json:"new_comment_count,omitempty"`
	NewCommentsSince         string                `json:"new_comments_since,omitempty"`
	ChatSessionID            string                `json:"chat_session_id,omitempty"`
	ChatMessage              string                `json:"chat_message,omitempty"`
	ChatMessageAttachments   []ChatAttachmentMeta  `json:"chat_message_attachments,omitempty"`
	AutopilotRunID           string                `json:"autopilot_run_id,omitempty"`
	AutopilotID              string                `json:"autopilot_id,omitempty"`
	AutopilotTitle           string                `json:"autopilot_title,omitempty"`
	AutopilotDescription     string                `json:"autopilot_description,omitempty"`
	AutopilotSource          string                `json:"autopilot_source,omitempty"`
	AutopilotTriggerPayload  json.RawMessage       `json:"autopilot_trigger_payload,omitempty"`
	QuickCreatePrompt        string                `json:"quick_create_prompt,omitempty"`
	QuickCreateAttachmentIDs []string              `json:"quick_create_attachment_ids,omitempty"`
	HandoffNote              string                `json:"handoff_note,omitempty"`

	SquadID               string `json:"squad_id,omitempty"`
	SquadName             string `json:"squad_name,omitempty"`
	ParentIssueID         string `json:"parent_issue_id,omitempty"`
	ParentIssueIdentifier string `json:"parent_issue_identifier,omitempty"`
	RequestingUserName               string `json:"requesting_user_name,omitempty"`
	RequestingUserProfileDescription string `json:"requesting_user_profile_description,omitempty"`
	InitiatorType  string `json:"initiator_type,omitempty"`
	InitiatorID    string `json:"initiator_id,omitempty"`
	InitiatorName  string `json:"initiator_name,omitempty"`
	InitiatorEmail string `json:"initiator_email,omitempty"`
	AuthToken string `json:"auth_token,omitempty"`
	LabelInstructions []LabelInstruction `json:"label_instructions,omitempty"`
}

// LabelInstruction carries a single label's agent-facing instructions.
type LabelInstruction struct {
	Name         string `json:"name"`
	Instructions string `json:"instructions"`
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
	ID            string            `json:"id"`
	Name          string            `json:"name"`
	Instructions  string            `json:"instructions"`
	Skills        []SkillData       `json:"skills,omitempty"`
	SkillRefs     []SkillRefData    `json:"skill_refs,omitempty"`
	CustomEnv     map[string]string `json:"custom_env,omitempty"`
	CustomArgs    []string          `json:"custom_args,omitempty"`
	McpConfig     json.RawMessage   `json:"mcp_config,omitempty"`
	Model         string            `json:"model,omitempty"`
	ThinkingLevel string            `json:"thinking_level,omitempty"`
	// RuntimeConfig is the per-provider runtime_config JSON as stored on
	// the agent record, forwarded verbatim by the claim endpoint. The
	// daemon decodes provider-specific fields (e.g. openclaw mode +
	// gateway endpoint, see issue #3260); other backends ignore it.
	RuntimeConfig json.RawMessage `json:"runtime_config,omitempty"`
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
