package protocol

import (
	"encoding/json"
	"time"
)

// ---------------------------------------------------------------------------
// Interaction types — requests that need human/system approval.
// ---------------------------------------------------------------------------

const (
	InteractionPermissionRequest  = "permission_request"
	InteractionCommandApproval    = "command_approval"
	InteractionFileChangeApproval = "file_change_approval"
	InteractionPlanApproval       = "plan_approval"
)

// Interaction status values.
const (
	InteractionStatusPending   = "pending"
	InteractionStatusApproved  = "approved"
	InteractionStatusDenied    = "denied"
	InteractionStatusTimedOut  = "timed_out"
	InteractionStatusCancelled = "cancelled"
)

// InteractionOption is a single selectable choice for an interaction.
type InteractionOption struct {
	ID    string `json:"id"`
	Label string `json:"label"`
}

// InteractionRequest is a pending approval request from an agent runtime.
type InteractionRequest struct {
	ID            string              `json:"id"`
	TaskID        string              `json:"task_id"`
	Provider      string              `json:"provider"`
	Type          string              `json:"type"`
	Title         string              `json:"title"`
	Detail        string              `json:"detail,omitempty"`
	Options       []InteractionOption `json:"options"`
	DefaultOption string              `json:"default_option,omitempty"`
	Status        string              `json:"status"`
	CreatedAt     time.Time           `json:"created_at"`
	ExpiresAt     time.Time           `json:"expires_at"`
	RespondedAt   *time.Time          `json:"responded_at,omitempty"`
	ChosenOption  string              `json:"chosen_option,omitempty"`
}

// InteractionResponse is the user/system answer to an InteractionRequest.
type InteractionResponse struct {
	RequestID    string `json:"request_id"`
	ChosenOption string `json:"chosen_option"`
}

// ---------------------------------------------------------------------------
// Approval policy — controls whether interactions are auto-approved.
// ---------------------------------------------------------------------------

const (
	ApprovalPolicyAuto   = "auto"   // silently approve (current default)
	ApprovalPolicyPrompt = "prompt" // surface to user for decision
	ApprovalPolicyDeny   = "deny"   // silently deny
)

// ResolveApprovalPolicy extracts approval_policy from an agent's runtime_config
// JSON blob. Returns ApprovalPolicyAuto if absent, empty, or unrecognised.
func ResolveApprovalPolicy(runtimeConfig []byte) string {
	if len(runtimeConfig) == 0 {
		return ApprovalPolicyAuto
	}
	var cfg struct {
		ApprovalPolicy string `json:"approval_policy"`
	}
	if err := json.Unmarshal(runtimeConfig, &cfg); err != nil || cfg.ApprovalPolicy == "" {
		return ApprovalPolicyAuto
	}
	switch cfg.ApprovalPolicy {
	case ApprovalPolicyPrompt, ApprovalPolicyDeny:
		return cfg.ApprovalPolicy
	default:
		return ApprovalPolicyAuto
	}
}

// ResolveTraceEnabled extracts trace_enabled from runtime_config. Tracing is
// enabled by default so users can observe agent work without changing approval
// behaviour; setting trace_enabled=false disables local trace capture/display.
func ResolveTraceEnabled(runtimeConfig []byte) bool {
	if len(runtimeConfig) == 0 {
		return true
	}
	var cfg struct {
		TraceEnabled *bool `json:"trace_enabled"`
	}
	if err := json.Unmarshal(runtimeConfig, &cfg); err != nil || cfg.TraceEnabled == nil {
		return true
	}
	return *cfg.TraceEnabled
}

// ---------------------------------------------------------------------------
// WebSocket / API event constants for interactions.
// ---------------------------------------------------------------------------

const (
	EventInteractionCreated  = "interaction:created"
	EventInteractionResolved = "interaction:resolved"
)

// ---------------------------------------------------------------------------
// Provider adapter interface — how each CLI backend handles approval requests.
//
// Implementations:
//   - AutoApprovalHandler: silently approves (current default, no behaviour change).
//   - DenyApprovalHandler: silently denies.
//   - PromptApprovalHandler: creates a pending interaction and blocks until
//     the user responds or the request times out.
//
// The daemon selects the handler based on ResolveApprovalPolicy(). Adapters
// for Claude, Codex, etc. translate the generic InteractionRequest/Response
// into their native protocol (control_response, approval ack, etc.).
// ---------------------------------------------------------------------------

// ApprovalHandler decides what to do when a provider emits an approval request.
type ApprovalHandler interface {
	// Handle processes an approval request. It returns the chosen option
	// (e.g. "allow", "deny") and whether the request was approved.
	// For auto/deny policies this returns immediately.
	// For prompt policy this blocks until a user responds or timeout.
	Handle(req InteractionRequest) (chosenOption string, approved bool)
}

// AutoApprovalHandler approves everything immediately.
type AutoApprovalHandler struct{}

func (AutoApprovalHandler) Handle(req InteractionRequest) (string, bool) {
	if req.DefaultOption != "" {
		return req.DefaultOption, true
	}
	return "allow", true
}

// DenyApprovalHandler denies everything immediately.
type DenyApprovalHandler struct{}

func (DenyApprovalHandler) Handle(_ InteractionRequest) (string, bool) {
	return "deny", false
}

// InteractionCreatedPayload is broadcast when a new interaction is registered.
type InteractionCreatedPayload struct {
	InteractionRequest
}

// InteractionResolvedPayload is broadcast when an interaction is resolved.
type InteractionResolvedPayload struct {
	RequestID    string `json:"request_id"`
	TaskID       string `json:"task_id"`
	Status       string `json:"status"`
	ChosenOption string `json:"chosen_option,omitempty"`
}
