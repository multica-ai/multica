package daemon

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

// Task represents a claimed task from the server.
// Agent data (name, skills) is populated by the claim endpoint.
type Task struct {
	ID             string     `json:"id"`
	AgentID        string     `json:"agent_id"`
	RuntimeID      string     `json:"runtime_id"`
	IssueID        *string    `json:"issue_id"`           // nil for agentflow tasks
	WorkspaceID    string     `json:"workspace_id"`
	Agent          *AgentData `json:"agent,omitempty"`
	Repos          []RepoData `json:"repos,omitempty"`
	PriorSessionID   string         `json:"prior_session_id,omitempty"`
	PriorWorkDir     string         `json:"prior_work_dir,omitempty"`
	TriggerCommentID string         `json:"trigger_comment_id,omitempty"`
	AgentflowRunID   *string        `json:"agentflow_run_id,omitempty"`
	Agentflow        *AgentflowData `json:"agentflow,omitempty"`
}

// AgentflowData holds agentflow context for agentflow-triggered tasks.
type AgentflowData struct {
	ID          string  `json:"id"`
	Title       string  `json:"title"`
	Description *string `json:"description"` // prompt template
	RunID       string  `json:"run_id"`
	SourceKind  string  `json:"source_kind"`
}

// IsAgentflow returns true if this task was triggered by an agentflow.
func (t Task) IsAgentflow() bool {
	return t.Agentflow != nil
}

// EffectiveIssueID returns the issue ID or empty string if nil.
func (t Task) EffectiveIssueID() string {
	if t.IssueID != nil {
		return *t.IssueID
	}
	return ""
}

// AgentData holds agent details returned by the claim endpoint.
type AgentData struct {
	ID           string      `json:"id"`
	Name         string      `json:"name"`
	Instructions string      `json:"instructions"`
	Skills       []SkillData `json:"skills"`
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

// TaskResult is the outcome of executing a task.
type TaskResult struct {
	Status     string `json:"status"`
	Comment    string `json:"comment"`
	BranchName string `json:"branch_name,omitempty"`
	EnvType    string `json:"env_type,omitempty"`
	SessionID  string `json:"session_id,omitempty"` // Claude session ID for future resumption
	WorkDir    string `json:"work_dir,omitempty"`   // working directory used during execution
}
