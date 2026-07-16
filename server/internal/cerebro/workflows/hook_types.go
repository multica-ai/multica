package workflows

import "time"

type HookEventType string

type HookDecision string

const (
	HookAllow   HookDecision = "allow"
	HookBlock   HookDecision = "block"
	HookModify  HookDecision = "modify"
	HookRequire HookDecision = "require"
)

type HookMode string

const (
	HookModeOff     HookMode = "off"
	HookModeDryRun  HookMode = "dry_run"
	HookModeEnforce HookMode = "enforce"
	HookModeManaged HookMode = "managed"
)

type HookFailMode string

const (
	HookFailOpen   HookFailMode = "open"
	HookFailClosed HookFailMode = "closed"
	HookFailWarn   HookFailMode = "warn"
)

type HookScopeKind string

const (
	HookScopeWorkspace HookScopeKind = "workspace"
	HookScopeProject   HookScopeKind = "project"
	HookScopeWorkflow  HookScopeKind = "workflow"
	HookScopeAgent     HookScopeKind = "agent"
	HookScopeModel     HookScopeKind = "model"
	HookScopeIssue     HookScopeKind = "issue"
	HookScopeSession   HookScopeKind = "session"
)

type HookBinding struct {
	Kind     HookScopeKind `json:"kind"`
	ID       string        `json:"id,omitempty"`
	Priority int           `json:"priority,omitempty"`
}

type HookHandler struct {
	ID            string         `json:"id"`
	Decision      HookDecision   `json:"decision"`
	Requirement   string         `json:"requirement,omitempty"`
	Modifications map[string]any `json:"modifications,omitempty"`
	Actions       []HookAction   `json:"actions,omitempty"`
}

type HookAction struct {
	Type   string         `json:"type"`
	Config map[string]any `json:"config,omitempty"`
}

type HookActionStatus string

const (
	HookActionWouldRun HookActionStatus = "would_run"
	HookActionSuccess  HookActionStatus = "success"
	HookActionFailed   HookActionStatus = "failed"
	HookActionDenied   HookActionStatus = "denied"
)

type HookActionResult struct {
	Type           string           `json:"type"`
	Status         HookActionStatus `json:"status"`
	IdempotencyKey string           `json:"idempotency_key"`
	HandlerID      string           `json:"handler_id,omitempty"`
	ActionIndex    int              `json:"action_index"`
	Config         map[string]any   `json:"-"`
	Result         map[string]any   `json:"result,omitempty"`
	Error          string           `json:"error,omitempty"`
}

type HookPolicy struct {
	ID            string          `json:"id"`
	Version       int             `json:"version"`
	Name          string          `json:"name"`
	Description   string          `json:"description,omitempty"`
	Mode          HookMode        `json:"mode"`
	FailMode      HookFailMode    `json:"fail_mode"`
	Events        []HookEventType `json:"events"`
	Bindings      []HookBinding   `json:"bindings"`
	Conditions    []Condition     `json:"conditions,omitempty"`
	Handlers      []HookHandler   `json:"handlers"`
	CreatedByID   string          `json:"created_by_id,omitempty"`
	CreatedByType string          `json:"created_by_type,omitempty"`
	UpdatedAt     time.Time       `json:"updated_at,omitempty"`
	LastRunAt     *time.Time      `json:"last_run_at,omitempty"`
	ObservedRuns  int             `json:"observed_run_count"`
	BaselineAt    *time.Time      `json:"baseline_at,omitempty"`
	CanPublish    bool            `json:"can_publish"`
}

type HookActor struct {
	Type string `json:"type,omitempty"`
	ID   string `json:"id,omitempty"`
}

type HookEvent struct {
	EventID       string         `json:"event_id"`
	Type          HookEventType  `json:"event_type"`
	WorkspaceID   string         `json:"workspace_id,omitempty"`
	ProjectID     string         `json:"project_id,omitempty"`
	WorkflowID    string         `json:"workflow_id,omitempty"`
	AgentID       string         `json:"agent_id,omitempty"`
	Model         string         `json:"model,omitempty"`
	IssueID       string         `json:"issue_id,omitempty"`
	SessionID     string         `json:"session_id,omitempty"`
	Actor         HookActor      `json:"actor,omitempty"`
	Proposed      map[string]any `json:"proposed_action,omitempty"`
	BeforeState   map[string]any `json:"before_state,omitempty"`
	Context       map[string]any `json:"context,omitempty"`
	MutableFields []string       `json:"mutable_fields,omitempty"`
	Attempt       int            `json:"attempt,omitempty"`
	NoProgress    int            `json:"no_progress,omitempty"`
	HookDepth     int            `json:"hook_depth,omitempty"`
}

type HookMatch struct {
	PolicyID    string       `json:"policy_id"`
	Version     int          `json:"version"`
	HandlerID   string       `json:"handler_id"`
	SourceScope HookBinding  `json:"source_scope"`
	Decision    HookDecision `json:"decision"`
	DryRun      bool         `json:"dry_run"`
	Idempotency string       `json:"idempotency_key"`
	MatchedAt   time.Time    `json:"matched_at"`
}

type HookResult struct {
	RunID             string             `json:"run_id,omitempty"`
	Decision          HookDecision       `json:"decision"`
	WouldDecision     HookDecision       `json:"would_decision,omitempty"`
	Requirements      []string           `json:"requirements,omitempty"`
	Modifications     map[string]any     `json:"modifications,omitempty"`
	Matches           []HookMatch        `json:"matches,omitempty"`
	MatchedConditions []Condition        `json:"matched_conditions,omitempty"`
	TimedOut          bool               `json:"timed_out,omitempty"`
	Warning           string             `json:"warning,omitempty"`
	ActionResults     []HookActionResult `json:"action_results,omitempty"`
}
