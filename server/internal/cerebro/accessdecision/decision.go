// Package accessdecision compares the live Gateway access decision with the
// future canonical access model. It is deliberately observational: callers
// persist and report the comparison, but never use the shadow result to allow
// or deny the current tool call.
package accessdecision

import "github.com/multica-ai/multica/server/internal/cerebro/availabilityevidence"

// Decision is the binary outcome compared by the ledger.
type Decision string

const (
	DecisionAllow Decision = "allow"
	DecisionDeny  Decision = "deny"
)

// PolicyDecision is the result returned by the canonical policy lookup. Ask
// and Error are recorded distinctly, but both fail closed inside the shadow.
type PolicyDecision string

const (
	PolicyAllow PolicyDecision = "allow"
	PolicyAsk   PolicyDecision = "ask"
	PolicyDeny  PolicyDecision = "deny"
	PolicyError PolicyDecision = "error"
)

// EvaluationInput is everything the pure shadow needs to reach a decision.
type EvaluationInput struct {
	CanonicalCapabilityID string
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
}

// EvaluationResult is the shadow outcome and its audit-safe explanation.
type EvaluationResult struct {
	Decision Decision
	Reason   string
}

// Evaluate applies the canonical shadow rules. Every incomplete or uncertain
// state denies inside the shadow. This result must never be used to enforce a
// live Gateway call during the measurement phase.
func Evaluate(in EvaluationInput) EvaluationResult {
	if in.CanonicalCapabilityID == "" {
		return EvaluationResult{Decision: DecisionDeny, Reason: "tool did not resolve to a canonical capability"}
	}
	if in.EvidenceLevel == availabilityevidence.LevelDeclared || in.EvidenceLevel == "" {
		return EvaluationResult{Decision: DecisionDeny, Reason: "canonical capability is absent from the live runtime surface"}
	}
	if in.PolicyDecision != PolicyAllow {
		return EvaluationResult{Decision: DecisionDeny, Reason: "canonical policy did not allow the capability"}
	}
	return EvaluationResult{Decision: DecisionAllow, Reason: "canonical policy allowed a capability present on the live runtime surface"}
}

// Observation is one live Gateway call plus the inputs used by the shadow.
type Observation struct {
	WorkspaceID           string
	AgentID               string
	RuntimeID             string
	OnBehalfOfUserID      string
	TaskID                string
	IssueID               string
	ObservedToolName      string
	CanonicalCapabilityID string
	LegacyDecision        Decision
	LegacyPath            string
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
}

// Entry is the append-only Decision Ledger record for one Gateway tool call.
type Entry struct {
	WorkspaceID           string
	AgentID               string
	RuntimeID             string
	OnBehalfOfUserID      string
	TaskID                string
	IssueID               string
	ObservedToolName      string
	CanonicalCapabilityID string
	LegacyDecision        Decision
	LegacyPath            string
	ShadowDecision        Decision
	PolicyDecision        PolicyDecision
	EvidenceLevel         availabilityevidence.Level
	Differs               bool
	Reason                string
}

// NewEntry evaluates the shadow and freezes the comparison for persistence.
func NewEntry(o Observation) Entry {
	shadow := Evaluate(EvaluationInput{
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
		LegacyDecision:        o.LegacyDecision,
		LegacyPath:            o.LegacyPath,
		ShadowDecision:        shadow.Decision,
		PolicyDecision:        o.PolicyDecision,
		EvidenceLevel:         o.EvidenceLevel,
		Differs:               o.LegacyDecision != shadow.Decision,
		Reason:                shadow.Reason,
	}
}
