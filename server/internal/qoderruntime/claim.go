package qoderruntime

import "encoding/json"

// Claim payload fields consumed by the remote bridge. Keep this HTTP boundary
// independent of the local daemon implementation and its CLI dependencies.
type claimedTask struct {
	ID                    string            `json:"id"`
	RuntimeID             string            `json:"runtime_id"`
	WorkspaceID           string            `json:"workspace_id"`
	IssueID               string            `json:"issue_id"`
	ChatSessionID         string            `json:"chat_session_id,omitempty"`
	ChatMessage           string            `json:"chat_message,omitempty"`
	ChatAttachments       []json.RawMessage `json:"chat_message_attachments,omitempty"`
	ChatChannelType       string            `json:"chat_channel_type,omitempty"`
	ChatType              string            `json:"chat_type,omitempty"`
	IsLeaderTask          bool              `json:"is_leader_task,omitempty"`
	AuthToken             string            `json:"auth_token,omitempty"`
	RemoteMCPConnections  []json.RawMessage `json:"remote_mcp_connections,omitempty"`
	ConnectedApps         []json.RawMessage `json:"connected_apps,omitempty"`
	PluginHookTools       []json.RawMessage `json:"plugin_hook_tools,omitempty"`
	Agent                 *claimedAgent     `json:"agent,omitempty"`
	PriorSessionID        string            `json:"prior_session_id,omitempty"`
	IssueIdentifier       string            `json:"issue_identifier,omitempty"`
	WorkspaceContext      string            `json:"workspace_context,omitempty"`
	ProjectTitle          string            `json:"project_title,omitempty"`
	ProjectDescription    string            `json:"project_description,omitempty"`
	HandoffNote           string            `json:"handoff_note,omitempty"`
	CoalescedComments     []claimedComment  `json:"coalesced_comments,omitempty"`
	TriggerCommentContent string            `json:"trigger_comment_content,omitempty"`
	TriggerAuthorName     string            `json:"trigger_author_name,omitempty"`
	ProjectResources      []claimedResource `json:"project_resources,omitempty"`
	Repos                 []claimedRepo     `json:"repos,omitempty"`

	QuickCreatePrompt        string          `json:"quick_create_prompt,omitempty"`
	QuickCreatePriority      string          `json:"quick_create_priority,omitempty"`
	QuickCreateDueDate       string          `json:"quick_create_due_date,omitempty"`
	QuickCreateAttachmentIDs []string        `json:"quick_create_attachment_ids,omitempty"`
	QuickCreateSourceContext json.RawMessage `json:"quick_create_source_context,omitempty"`
	ProjectID                string          `json:"project_id,omitempty"`
	ParentIssueID            string          `json:"parent_issue_id,omitempty"`
	SquadID                  string          `json:"squad_id,omitempty"`
}

type claimedAgent struct {
	ID            string            `json:"id"`
	Instructions  string            `json:"instructions"`
	Skills        []claimedSkill    `json:"skills,omitempty"`
	SkillRefs     []json.RawMessage `json:"skill_refs,omitempty"`
	CustomEnv     map[string]string `json:"custom_env,omitempty"`
	CustomArgs    []string          `json:"custom_args,omitempty"`
	McpConfig     json.RawMessage   `json:"mcp_config,omitempty"`
	Model         string            `json:"model,omitempty"`
	ThinkingLevel string            `json:"thinking_level,omitempty"`
	ServiceTier   string            `json:"service_tier,omitempty"`
	RuntimeConfig json.RawMessage   `json:"runtime_config,omitempty"`
}

type claimedSkill struct {
	ID string `json:"id"`
}
type claimedComment struct {
	AuthorName string `json:"author_name"`
	Content    string `json:"content"`
}
type claimedResource struct {
	ResourceType string          `json:"resource_type"`
	ResourceRef  json.RawMessage `json:"resource_ref"`
}
type claimedRepo struct {
	URL         string `json:"url"`
	Description string `json:"description"`
}
