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
}

// RepoData holds repository information from the workspace.
type RepoData struct {
	URL         string `json:"url"`
	Description string `json:"description"`
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
	ID                      string          `json:"id"`
	AgentID                 string          `json:"agent_id"`
	RuntimeID               string          `json:"runtime_id"`
	IssueID                 string          `json:"issue_id"`
	WorkspaceID             string          `json:"workspace_id"`
	Agent                   *AgentData      `json:"agent,omitempty"`
	Repos                   []RepoData            `json:"repos,omitempty"`
	ProjectID               string                `json:"project_id,omitempty"`        // issue's project, when present
	ProjectTitle            string                `json:"project_title,omitempty"`     // human-readable project title for context injection
	ProjectResources        []ProjectResourceData `json:"project_resources,omitempty"` // project-scoped resources to expose to the agent
	PriorSessionID          string          `json:"prior_session_id,omitempty"`          // Claude session ID from a previous task on this issue
	PriorWorkDir            string          `json:"prior_work_dir,omitempty"`            // work_dir from a previous task on this issue
	TriggerCommentID        string          `json:"trigger_comment_id,omitempty"`        // comment that triggered this task
	TriggerCommentContent   string          `json:"trigger_comment_content,omitempty"`   // content of the triggering comment
	TriggerAuthorType       string          `json:"trigger_author_type,omitempty"`       // "agent" or "member" — author kind for the triggering comment
	TriggerAuthorName       string          `json:"trigger_author_name,omitempty"`       // display name of the triggering comment author
	ChatSessionID           string          `json:"chat_session_id,omitempty"`           // non-empty for chat tasks
	ChatMessage             string          `json:"chat_message,omitempty"`              // user message content for chat tasks
	AutopilotRunID          string          `json:"autopilot_run_id,omitempty"`          // non-empty for autopilot run_only tasks
	AutopilotID             string          `json:"autopilot_id,omitempty"`              // autopilot that spawned this run
	AutopilotTitle          string          `json:"autopilot_title,omitempty"`           // autopilot title used as task context
	AutopilotDescription    string          `json:"autopilot_description,omitempty"`     // autopilot description used as task prompt
	AutopilotSource         string          `json:"autopilot_source,omitempty"`          // manual, schedule, webhook, or api
	AutopilotTriggerPayload json.RawMessage `json:"autopilot_trigger_payload,omitempty"` // optional trigger payload for webhook/api runs
	QuickCreatePrompt       string          `json:"quick_create_prompt,omitempty"`       // user's natural-language input for quick-create tasks
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
	FailureReason string           `json:"-"`                    // internal server failure classification
	Usage         []TaskUsageEntry `json:"usage,omitempty"`      // per-model token usage
}
