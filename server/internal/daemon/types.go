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

// PairingSession represents a daemon pairing session.
type PairingSession struct {
	Token          string  `json:"token"`
	DaemonID       string  `json:"daemon_id"`
	DeviceName     string  `json:"device_name"`
	RuntimeName    string  `json:"runtime_name"`
	RuntimeType    string  `json:"runtime_type"`
	RuntimeVersion string  `json:"runtime_version"`
	WorkspaceID    *string `json:"workspace_id"`
	Status         string  `json:"status"`
	ApprovedAt     *string `json:"approved_at"`
	ClaimedAt      *string `json:"claimed_at"`
	ExpiresAt      string  `json:"expires_at"`
	LinkURL        *string `json:"link_url"`
}

// PersistedConfig is the JSON structure saved to ~/.multica/daemon.json.
type PersistedConfig struct {
	WorkspaceID string `json:"workspace_id"`
}

// Task represents a claimed task from the server.
type Task struct {
	ID      string      `json:"id"`
	AgentID string      `json:"agent_id"`
	IssueID string      `json:"issue_id"`
	Context TaskContext `json:"context"`
}

// TaskContext contains the snapshot context for a task.
type TaskContext struct {
	Issue            IssueContext   `json:"issue"`
	Agent            AgentContext   `json:"agent"`
	Runtime          RuntimeContext `json:"runtime"`
	WorkspaceContext string         `json:"workspace_context,omitempty"`
}

// IssueContext holds issue details for task execution.
type IssueContext struct {
	ID                 string   `json:"id"`
	Title              string   `json:"title"`
	Description        string   `json:"description"`
	AcceptanceCriteria []string `json:"acceptance_criteria"`
	ContextRefs        []string `json:"context_refs"`
	Repository         *RepoRef `json:"repository"`
}

// AgentContext holds agent details for task execution.
type AgentContext struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Skills string `json:"skills"`
}

// RuntimeContext holds runtime details for task execution.
type RuntimeContext struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Provider   string `json:"provider"`
	DeviceInfo string `json:"device_info"`
}

// RepoRef points to a repository for an issue.
type RepoRef struct {
	URL    string `json:"url"`
	Branch string `json:"branch"`
	Path   string `json:"path"`
}

// TaskResult is the outcome of executing a task.
type TaskResult struct {
	Status  string `json:"status"`
	Comment string `json:"comment"`
}
