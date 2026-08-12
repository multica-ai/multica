package service

import (
	"testing"

	"github.com/multica-ai/multica/server/pkg/protocol"
)

func TestCompletionGateAllFivePresent(t *testing.T) {
	in := CompletionInput{
		OutputPresent: true, Output: "done",
		MessageCount: 2, UsagePresent: true,
		Artifacts: map[string]ArtifactEvidence{
			"a1": {RefPresent: true, SHA256Present: true},
		},
		Tests: map[string]bool{"t1": true},
	}
	res := EvaluateCompletionGate(in)
	if !res.Pass {
		t.Fatalf("gate must pass: %+v", res)
	}
}

func TestCompletionGateMissingEach(t *testing.T) {
	base := CompletionInput{
		OutputPresent: true, Output: "done",
		MessageCount: 2, UsagePresent: true,
		Artifacts: map[string]ArtifactEvidence{"a1": {RefPresent: true, SHA256Present: true}},
		Tests:     map[string]bool{"t1": true},
	}
	cases := []struct {
		name string
		mut  func(*CompletionInput)
		want MissingEvidenceCategory
	}{
		{"output", func(c *CompletionInput) { c.Output = "" }, MissingOutput},
		{"message", func(c *CompletionInput) { c.MessageCount = 0 }, MissingMessage},
		{"usage", func(c *CompletionInput) { c.UsagePresent = false }, MissingUsage},
		{"artifact", func(c *CompletionInput) { c.Artifacts["a1"] = ArtifactEvidence{RefPresent: true} }, MissingArtifact},
		{"tests", func(c *CompletionInput) { c.Tests["t1"] = false }, MissingTests},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := CompletionInput{
				OutputPresent: base.OutputPresent, Output: base.Output,
				MessageCount: base.MessageCount, UsagePresent: base.UsagePresent,
				Artifacts: make(map[string]ArtifactEvidence, len(base.Artifacts)),
				Tests:     make(map[string]bool, len(base.Tests)),
			}
			for k, v := range base.Artifacts {
				in.Artifacts[k] = v
			}
			for k, v := range base.Tests {
				in.Tests[k] = v
			}
			tc.mut(&in)
			res := EvaluateCompletionGate(in)
			if res.Pass {
				t.Fatal("gate must fail")
			}
			if res.Missing != tc.want {
				t.Fatalf("missing = %s, want %s", res.Missing, tc.want)
			}
		})
	}
}

func TestCompletionGateReviewNotACategory(t *testing.T) {
	// Missing review must NOT turn a valid runtime completion into failure
	// (V4-5.1). The completion gate has no review input at all.
	in := CompletionInput{
		OutputPresent: true, Output: "done",
		MessageCount: 1, UsagePresent: true,
	}
	res := EvaluateCompletionGate(in)
	if !res.Pass {
		t.Fatalf("valid runtime evidence must pass regardless of review: %+v", res)
	}
}

func TestInitialReviewStateNone(t *testing.T) {
	s := ComputeInitialReviewState(ReviewInitialInput{Policy: protocol.ReviewPolicyNone})
	if s.State != protocol.ReviewStateNotRequired {
		t.Fatalf("state = %s, want not_required", s.State)
	}
	if s.NextWakeupNow || s.ReviewerAgentID != nil || s.FailureCode != nil {
		t.Fatalf("not_required must carry null refs: %+v", s)
	}
}

func TestInitialReviewStatePending(t *testing.T) {
	s := ComputeInitialReviewState(ReviewInitialInput{
		Policy:           protocol.ReviewPolicyIndependent,
		ReviewerAgentID:  "agent-2",
		ExecutionAgentID: "agent-1",
		ReviewerValid:    true,
	})
	if s.State != protocol.ReviewStatePending {
		t.Fatalf("state = %s, want pending", s.State)
	}
	if !s.NextWakeupNow {
		t.Fatal("pending must be immediately claimable")
	}
}

func TestInitialReviewStateBlockedBranches(t *testing.T) {
	cases := []struct {
		name string
		in   ReviewInitialInput
		want string
	}{
		{"no reviewer", ReviewInitialInput{Policy: protocol.ReviewPolicyIndependent, ExecutionAgentID: "a1"}, "memoryhub_reviewer_unavailable"},
		{"self", ReviewInitialInput{Policy: protocol.ReviewPolicyIndependent, ReviewerAgentID: "a1", ExecutionAgentID: "a1", ReviewerValid: true}, "memoryhub_reviewer_self_forbidden"},
		{"scope mismatch", ReviewInitialInput{Policy: protocol.ReviewPolicyIndependent, ReviewerAgentID: "a2", ExecutionAgentID: "a1", ReviewerValid: false}, "memoryhub_reviewer_scope_mismatch"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := ComputeInitialReviewState(tc.in)
			if s.State != protocol.ReviewStateBlocked {
				t.Fatalf("state = %s, want blocked", s.State)
			}
			if s.FailureCode == nil || *s.FailureCode != tc.want {
				t.Fatalf("failure = %v, want %s", s.FailureCode, tc.want)
			}
			if s.NextWakeupNow {
				t.Fatal("blocked must never carry a scheduler wakeup")
			}
		})
	}
}

func TestReviewBackoff(t *testing.T) {
	want := []int{1, 2, 4, 8, 16, 32, 32}
	for i, w := range want {
		if got := reviewTransitionBackoff(i + 1); got != w {
			t.Fatalf("backoff[%d] = %d, want %d", i+1, got, w)
		}
	}
}
