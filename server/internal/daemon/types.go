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
//
// Two forms coexist:
//   - Type == "github": URL is the remote (https or ssh), daemon bare-clones
//     into its cache the first time and creates worktrees from there.
//   - Type == "local": LocalPath points at a directory on the daemon host
//     that already has its own .git (regular repo or a worktree). The daemon
//     creates worktrees directly off that repo's git-common-dir so existing
//     remotes, refs, and history are shared with the user's own checkout.
//
// ID is stable across workspace.repos updates and is how a project's
// project_repo rows point at an entry.
type RepoData struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	URL         string `json:"url,omitempty"`
	LocalPath   string `json:"local_path,omitempty"`
	Description string `json:"description"`
}

// IsLocal reports whether this repo entry references a local filesystem path.
func (r RepoData) IsLocal() bool { return r.Type == "local" }

// IsGitHub reports whether this repo entry references a remote URL. The empty
// type is treated as GitHub for backward compatibility with pre-migration-040
// rows.
func (r RepoData) IsGitHub() bool { return r.Type == "" || r.Type == "github" }

// Source returns the origin the daemon should clone or worktree from.
// For GitHub repos this is the URL; for local repos it's the absolute path.
func (r RepoData) Source() string {
	if r.IsLocal() {
		return r.LocalPath
	}
	return r.URL
}

// Task represents a claimed task from the server.
// Agent data (name, skills) is populated by the claim endpoint.
type Task struct {
	ID             string     `json:"id"`
	AgentID        string     `json:"agent_id"`
	RuntimeID      string     `json:"runtime_id"`
	IssueID        string     `json:"issue_id"`
	WorkspaceID    string     `json:"workspace_id"`
	Agent          *AgentData `json:"agent,omitempty"`
	Repos          []RepoData `json:"repos,omitempty"`
	PriorSessionID   string     `json:"prior_session_id,omitempty"`    // Claude session ID from a previous task on this issue
	PriorWorkDir     string     `json:"prior_work_dir,omitempty"`     // work_dir from a previous task on this issue
	TriggerCommentID string     `json:"trigger_comment_id,omitempty"` // comment that triggered this task
	ChatSessionID    string     `json:"chat_session_id,omitempty"`    // non-empty for chat tasks
	ChatMessage      string     `json:"chat_message,omitempty"`       // user message content for chat tasks
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
	Status     string           `json:"status"`
	Comment    string           `json:"comment"`
	BranchName string           `json:"branch_name,omitempty"`
	EnvType    string           `json:"env_type,omitempty"`
	SessionID  string           `json:"session_id,omitempty"` // Claude session ID for future resumption
	WorkDir    string           `json:"work_dir,omitempty"`   // working directory used during execution
	Usage      []TaskUsageEntry `json:"usage,omitempty"`      // per-model token usage
}
