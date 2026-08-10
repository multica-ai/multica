package workflows

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

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

type HookConditionMode string

const (
	HookConditionAll HookConditionMode = "all"
	HookConditionAny HookConditionMode = "any"
)

type HookFailMode string

const (
	HookFailClosed HookFailMode = "closed"
	HookFailWarn   HookFailMode = "warn"
)

type HookLifecycleState string

const (
	HookLifecycleDraft         HookLifecycleState = "draft"
	HookLifecycleLive          HookLifecycleState = "live"
	HookLifecycleLiveWithDraft HookLifecycleState = "live_with_draft"
	HookLifecycleOff           HookLifecycleState = "off"
	HookLifecycleOffWithDraft  HookLifecycleState = "off_with_draft"
	HookLifecycleManaged       HookLifecycleState = "managed"
)

type HookLifecycle struct {
	State                HookLifecycleState `json:"state"`
	LivePolicyID         string             `json:"live_policy_id,omitempty"`
	LiveVersion          int                `json:"live_version,omitempty"`
	DraftID              string             `json:"draft_id,omitempty"`
	DraftSeriesID        string             `json:"draft_series_id,omitempty"`
	DraftRevision        int                `json:"draft_revision,omitempty"`
	LiveUnchangedByDraft bool               `json:"live_unchanged_by_draft"`
}

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
	ID               string             `json:"id"`
	FamilyID         string             `json:"family_id"`
	DraftSeriesID    string             `json:"draft_series_id,omitempty"`
	Revision         int                `json:"revision,omitempty"`
	Version          int                `json:"version"`
	Name             string             `json:"name"`
	Description      string             `json:"description,omitempty"`
	ContractRule     string             `json:"contract_rule"`
	ContractSatisfy  string             `json:"contract_satisfy"`
	Mode             HookMode           `json:"mode"`
	FailMode         HookFailMode       `json:"fail_mode"`
	Events           []HookEventType    `json:"events"`
	Bindings         []HookBinding      `json:"bindings"`
	ConditionMode    HookConditionMode  `json:"condition_mode"`
	Conditions       []Condition        `json:"conditions,omitempty"`
	Handlers         []HookHandler      `json:"handlers"`
	CreatedByID      string             `json:"created_by_id,omitempty"`
	CreatedByType    string             `json:"created_by_type,omitempty"`
	UpdatedAt        time.Time          `json:"updated_at,omitempty"`
	LastRunAt        *time.Time         `json:"last_run_at,omitempty"`
	ObservedRuns     int                `json:"observed_run_count"`
	PassCount7D      int                `json:"pass_count_7d"`
	BlockCount7D     int                `json:"block_count_7d"`
	BaselineAt       *time.Time         `json:"baseline_at,omitempty"`
	CanPublish       bool               `json:"can_publish"`
	Lifecycle        HookLifecycle      `json:"lifecycle"`
	CompatibleEvents []HookJournalEvent `json:"compatible_events,omitempty"`
	// Live describes the version that is actually enforcing right now. It is
	// set only when the configuration above is a Draft, because in that case
	// every other field describes something that is NOT running yet.
	Live *HookPolicyLive `json:"live,omitempty"`
}

type HookPolicyLive struct {
	PolicyID    string       `json:"policy_id"`
	Version     int          `json:"version"`
	Name        string       `json:"name"`
	Decision    HookDecision `json:"decision"`
	Requirement string       `json:"requirement,omitempty"`
	FailMode    HookFailMode `json:"fail_mode"`
}

func (p *HookPolicy) UnmarshalJSON(data []byte) error {
	type hookPolicyAlias HookPolicy
	decoded := hookPolicyAlias{ConditionMode: HookConditionAll}
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	if decoded.ConditionMode == "" {
		decoded.ConditionMode = HookConditionAll
	}
	*p = HookPolicy(decoded)
	return nil
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
	PolicyName  string       `json:"policy_name,omitempty"`
	Version     int          `json:"version"`
	HandlerID   string       `json:"handler_id"`
	SourceScope HookBinding  `json:"source_scope"`
	Decision    HookDecision `json:"decision"`
	DryRun      bool         `json:"dry_run"`
	Idempotency string       `json:"idempotency_key"`
	MatchedAt   time.Time    `json:"matched_at"`
}

// HookRef names the enforcing policy behind a decision. Every user-visible
// message about a stopped action carries it, so nobody has to guess which of
// the workspace's hooks made the call.
type HookRef struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

func (r HookRef) Label() string {
	if r.Name != "" {
		return r.Name
	}
	return r.ID
}

type HookResult struct {
	RunID             string             `json:"run_id,omitempty"`
	Evaluated         bool               `json:"evaluated"`
	Decision          HookDecision       `json:"decision"`
	BlockedBy         *HookRef           `json:"blocked_by,omitempty"`
	WouldDecision     HookDecision       `json:"would_decision,omitempty"`
	Requirements      []string           `json:"requirements,omitempty"`
	Modifications     map[string]any     `json:"modifications,omitempty"`
	Matches           []HookMatch        `json:"matches,omitempty"`
	MatchedConditions []Condition        `json:"matched_conditions,omitempty"`
	TimedOut          bool               `json:"timed_out,omitempty"`
	Warning           string             `json:"warning,omitempty"`
	ActionResults     []HookActionResult `json:"action_results,omitempty"`
}

// blockingHook finds the enforcing policy that raised the decision to block or
// require. Dry-run matches are skipped: they never stop anything, so naming one
// would point at an innocent hook.
func (r HookResult) blockingHook() *HookRef {
	for _, match := range r.Matches {
		if !match.DryRun && (match.Decision == HookBlock || match.Decision == HookRequire) {
			return &HookRef{ID: match.PolicyID, Name: match.PolicyName}
		}
	}
	return nil
}

// ReasonWithHook is the one place a stopped action turns into a sentence a
// human can act on. It always names the hook when one is known, so "workflow
// hook blocked before.session.start" can never reach a user again.
func (r HookResult) ReasonWithHook(detail string) string {
	detail = strings.TrimSpace(detail)
	if r.BlockedBy == nil {
		return detail
	}
	if detail == "" {
		return fmt.Sprintf("Hook %q stopped this.", r.BlockedBy.Label())
	}
	return fmt.Sprintf("Hook %q stopped this: %s", r.BlockedBy.Label(), detail)
}
