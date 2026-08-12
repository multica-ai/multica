// Package service: H6 evidence collector and completion gate (Plan v1.4
// V4-5.1 + v1.5 V5-7). Owner: ALL-16.
//
// CompleteTaskWithRuntimeEvidenceGate validates five runtime-owned categories:
// non-empty output, at least one message, usage with provider/model, every
// required artifact with ref/SHA-256, and required tests passing. If any is
// absent the task follows the existing failure/retry path with a specific
// missing-evidence code; it never transiently becomes completed. Missing
// independent review never converts a valid runtime completion into failure.
package service

import (
	"errors"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

// MissingEvidenceCategory identifies which of the five runtime categories is
// absent; it maps to the ledger stop_reason.
type MissingEvidenceCategory string

const (
	MissingOutput    MissingEvidenceCategory = "empty_or_unparseable_output"
	MissingMessage   MissingEvidenceCategory = "missing_persisted_message"
	MissingUsage     MissingEvidenceCategory = "missing_provider_model_usage"
	MissingArtifact  MissingEvidenceCategory = "missing_required_artifact_ref_sha256"
	MissingTests     MissingEvidenceCategory = "missing_required_test_result"
)

// CompletionInput is the frozen input to the completion gate.
type CompletionInput struct {
	OutputPresent  bool
	Output         string
	MessageCount   int
	UsagePresent   bool
	// Artifacts maps required artifact ref -> (ref present && sha256 present).
	Artifacts map[string]ArtifactEvidence
	// Tests maps required test ref -> passed.
	Tests map[string]bool
}

// ArtifactEvidence is one artifact's ref+SHA-256 presence.
type ArtifactEvidence struct {
	RefPresent   bool
	SHA256Present bool
}

// CompletionGateResult is the gate verdict.
type CompletionGateResult struct {
	// Pass is true only when all five categories are satisfied.
	Pass bool
	// Missing is the first missing category when !Pass; empty when Pass.
	Missing MissingEvidenceCategory
}

// EvaluateCompletionGate applies the five-category runtime completion gate.
// Missing review is NOT one of the five categories (V4-5.1).
func EvaluateCompletionGate(in CompletionInput) CompletionGateResult {
	if !in.OutputPresent || in.Output == "" {
		return CompletionGateResult{Pass: false, Missing: MissingOutput}
	}
	if in.MessageCount < 1 {
		return CompletionGateResult{Pass: false, Missing: MissingMessage}
	}
	if !in.UsagePresent {
		return CompletionGateResult{Pass: false, Missing: MissingUsage}
	}
	for _, art := range in.Artifacts {
		if !art.RefPresent || !art.SHA256Present {
			return CompletionGateResult{Pass: false, Missing: MissingArtifact}
		}
	}
	for _, passed := range in.Tests {
		if !passed {
			return CompletionGateResult{Pass: false, Missing: MissingTests}
		}
	}
	return CompletionGateResult{Pass: true}
}

// InitialReviewState computes the V5-7.1 frozen initial review state for an
// execution record at runtime completion.
//
//   - policy none -> not_required (terminal for review; all refs null, attempt 0).
//   - independent with a valid same-workspace, non-self reviewer -> pending
//     (attempt 0, wakeup=now).
//   - independent with no reviewer -> blocked memoryhub_reviewer_unavailable,
//     wakeup=null.
//   - independent with the execution agent as reviewer -> blocked
//     memoryhub_reviewer_self_forbidden, wakeup=null.
//   - independent with a missing/deleted/cross-workspace reviewer -> blocked
//     memoryhub_reviewer_scope_mismatch, wakeup=null.
//
// blocked never means automatic retry and never carries a scheduler wakeup.
type InitialReviewState struct {
	State            protocol.ReviewState
	ReviewerAgentID  *string
	FailureCode      *string
	NextWakeupNow    bool // pending: scheduler may claim immediately
}

// ReviewInitialInput is the input to initial-state computation.
type ReviewInitialInput struct {
	Policy            protocol.ReviewPolicyMode
	ReviewerAgentID   string // proposed reviewer, empty when none
	ExecutionAgentID  string
	ReviewerValid     bool // exists, active, same workspace
}

func strPtr(s string) *string { return &s }

// ComputeInitialReviewState applies the V5-7.1 frozen table.
func ComputeInitialReviewState(in ReviewInitialInput) InitialReviewState {
	if in.Policy == protocol.ReviewPolicyNone {
		return InitialReviewState{State: protocol.ReviewStateNotRequired}
	}
	// policy == independent
	switch {
	case in.ReviewerAgentID == "":
		return InitialReviewState{
			State:       protocol.ReviewStateBlocked,
			FailureCode: strPtr("memoryhub_reviewer_unavailable"),
		}
	case in.ReviewerAgentID == in.ExecutionAgentID:
		return InitialReviewState{
			State:       protocol.ReviewStateBlocked,
			FailureCode: strPtr("memoryhub_reviewer_self_forbidden"),
		}
	case !in.ReviewerValid:
		return InitialReviewState{
			State:       protocol.ReviewStateBlocked,
			FailureCode: strPtr("memoryhub_reviewer_scope_mismatch"),
		}
	default:
		return InitialReviewState{
			State:           protocol.ReviewStatePending,
			ReviewerAgentID: strPtr(in.ReviewerAgentID),
			NextWakeupNow:   true,
		}
	}
}

// reviewTransitionBackoff is the frozen transient backoff in seconds
// (V5-7.2): 1s,2s,4s,8s,16s,32s, capped by max_review_attempts.
func reviewTransitionBackoff(attempt int) int {
	if attempt <= 0 {
		return 1
	}
	if attempt > 6 {
		return 32
	}
	return 1 << (attempt - 1)
}

var _ = errors.Is
