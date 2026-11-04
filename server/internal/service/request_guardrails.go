package service

import "fmt"

// Guardrails for the Secretary's requests (spec "Guardrails", step 13.7.1).
// They stop a runaway, not a style. Each limit is a constant here; a workspace
// setting that overrides one is out of scope for this step.

// GuardrailKind names one limit, so a refusal can say which one was hit.
type GuardrailKind string

const (
	// GuardrailAddressees caps the squads and leaders one request is sent to.
	GuardrailAddressees GuardrailKind = "addressees_per_request"
	// GuardrailOpenRequests caps the requests a workspace has open at once.
	GuardrailOpenRequests GuardrailKind = "open_requests_per_workspace"
	// GuardrailChallenges caps the challenge comments the Secretary posts on one child issue.
	GuardrailChallenges GuardrailKind = "challenges_per_child_issue"
	// GuardrailDirectives caps the directives created from one boss message.
	GuardrailDirectives GuardrailKind = "directives_per_boss_message"
	// GuardrailDelegationDepth caps the hops from the Secretary down (Secretary to chief to leader is two).
	GuardrailDelegationDepth GuardrailKind = "delegation_depth"
)

const (
	MaxRequestAddressees        = 20
	MaxOpenRequestsPerWorkspace = 8
	MaxChallengesPerChildIssue  = 8
	MaxDirectivesPerBossMessage = 5
	MaxDelegationDepth          = 2
)

// GuardrailMax returns the limit for a kind, or 0 for an unknown kind.
func GuardrailMax(kind GuardrailKind) int {
	switch kind {
	case GuardrailAddressees:
		return MaxRequestAddressees
	case GuardrailOpenRequests:
		return MaxOpenRequestsPerWorkspace
	case GuardrailChallenges:
		return MaxChallengesPerChildIssue
	case GuardrailDirectives:
		return MaxDirectivesPerBossMessage
	case GuardrailDelegationDepth:
		return MaxDelegationDepth
	}
	return 0
}

// GuardrailError is returned when a limit would be exceeded. Its message is
// plain English, because the Secretary reads it and explains it to the boss.
type GuardrailError struct {
	Kind GuardrailKind
	Max  int
	// Count is the value the caller proposed, including the item being added.
	Count int
}

func (e *GuardrailError) Error() string {
	return fmt.Sprintf("limit reached: %s allows at most %d (this would make %d)", e.Kind, e.Max, e.Count)
}

// CheckGuardrail reports whether proposed stays within the limit for kind.
// proposed is the total after the change, so a caller adding one directive to
// four existing ones passes 5, and 6 is refused. An unknown kind is refused
// rather than waved through.
func CheckGuardrail(kind GuardrailKind, proposed int) error {
	limit := GuardrailMax(kind)
	if limit == 0 {
		return &GuardrailError{Kind: kind, Max: 0, Count: proposed}
	}
	if proposed > limit {
		return &GuardrailError{Kind: kind, Max: limit, Count: proposed}
	}
	return nil
}
