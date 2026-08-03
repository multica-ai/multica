// Package accessdecision is the Gateway's canonical permission and
// availability decision contract.
package accessdecision

import "github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"

// Decision is the binary outcome enforced by the Gateway.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// PolicyDecision is the result returned by the canonical policy lookup. Ask
// and Error remain distinct so callers can route Ask through approvals while
// both fail closed when no approval flow is available.
type PolicyDecision string

// ReasonCode is the machine-readable authority/recovery classification stored
// with each decision. Reason remains display-only copy.
type ReasonCode string

const (
	PolicyAllow PolicyDecision = "allow"
	PolicyAsk   PolicyDecision = "ask"
	PolicyDeny  PolicyDecision = "deny"
	PolicyError PolicyDecision = "error"
)

const (
	ReasonLegacyUnknown                 ReasonCode = "legacy_unknown"
	ReasonAllowed                       ReasonCode = "policy_allowed"
	ReasonCanonicalCapabilityUnresolved ReasonCode = "canonical_capability_unresolved"
	ReasonRuntimeCapabilityUnavailable  ReasonCode = "runtime_capability_unavailable"
	ReasonPolicyApprovalRequired        ReasonCode = "policy_approval_required"
	ReasonPolicyDenied                  ReasonCode = "policy_denied"
	ReasonPolicyUnavailable             ReasonCode = "policy_unavailable"
)

// EvaluationInput is everything the pure decision needs.
type EvaluationInput struct {
	CanonicalCapabilityID string
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
}

// EvaluationResult is the enforced outcome and its audit-safe explanation.
type EvaluationResult struct {
	Decision   Decision
	ReasonCode ReasonCode
	Reason     string
}

// Evaluate applies the canonical fail-closed rules.
func Evaluate(in EvaluationInput) EvaluationResult {
	if in.CanonicalCapabilityID == "" {
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonCanonicalCapabilityUnresolved, Reason: "tool did not resolve to a canonical capability"}
	}
	if in.EvidenceLevel == availabilityevidence.LevelDeclared || in.EvidenceLevel == "" {
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonRuntimeCapabilityUnavailable, Reason: "canonical capability is absent from the live runtime surface"}
	}
	switch in.PolicyDecision {
	case PolicyAsk:
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonPolicyApprovalRequired, Reason: "canonical policy requires approval for the capability"}
	case PolicyDeny:
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonPolicyDenied, Reason: "canonical policy did not allow the capability"}
	case PolicyError:
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonPolicyUnavailable, Reason: "canonical policy decision was unavailable"}
	case PolicyAllow:
		return EvaluationResult{Decision: DecisionAllow, ReasonCode: ReasonAllowed, Reason: "canonical policy allowed a capability present on the live runtime surface"}
	default:
		return EvaluationResult{Decision: DecisionDeny, ReasonCode: ReasonPolicyUnavailable, Reason: "canonical policy decision was unavailable"}
	}
}

// Observation is one live Gateway call plus the canonical inputs.
type Observation struct {
	WorkspaceID           string
	AgentID               string
	RuntimeID             string
	OnBehalfOfUserID      string
	TaskID                string
	IssueID               string
	ObservedToolName      string
	CanonicalCapabilityID string
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
}

// Entry is the append-only canonical decision record for one Gateway tool call.
type Entry struct {
	WorkspaceID           string
	AgentID               string
	RuntimeID             string
	OnBehalfOfUserID      string
	TaskID                string
	IssueID               string
	ObservedToolName      string
	CanonicalCapabilityID string
	Decision              Decision
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
	ReasonCode            ReasonCode
	Reason                string
}

// NewEntry evaluates the canonical decision and freezes it for persistence.
func NewEntry(o Observation) Entry {
	result := Evaluate(EvaluationInput{
		CanonicalCapabilityID: o.CanonicalCapabilityID,
		PolicyDecision:        o.PolicyDecision,
		EvidenceLevel:         o.EvidenceLevel,
	})
	return Entry{
		WorkspaceID:           o.WorkspaceID,
		AgentID:               o.AgentID,
		RuntimeID:             o.RuntimeID,
		OnBehalfOfUserID:      o.OnBehalfOfUserID,
		TaskID:                o.TaskID,
		IssueID:               o.IssueID,
		ObservedToolName:      o.ObservedToolName,
		CanonicalCapabilityID: o.CanonicalCapabilityID,
		Decision:              result.Decision,
		PolicyDecision:        o.PolicyDecision,
		EvidenceLevel:         o.EvidenceLevel,
		ReasonCode:            result.ReasonCode,
		Reason:                result.Reason,
	}
}
