package github

import "testing"

// Tests in this file are scenario-named assertions for the CR-flow
// invariants after the Phase 1 redesign (post-2026-05-03):
//
//   1. Comment-only streams are silent mirrors. Wrapping review events are
//      the only CR signals that drive status.
//   2. review_submitted (APPROVED) is the SOLE autonomous → staged path.
//      COMMENTED with predicate-clean noops; predicate-clear thread events
//      also noop (req: APPROVED only → staged).
//   3. review_submitted with unresolved → resolving.
//
// The state-machine logic is also covered by the broader transition tests
// in state_machine_test.go; these are documentation tests scoped to the
// CR loop so coverage is greppable.

// 2. review_submitted (APPROVED) with zero unresolved → staged (skips in_review).
//
// Note: a COMMENTED review with predicate-clean noops here — see
// TestDecide_ReviewClean's race-guard subtests. APPROVED is the explicit
// "promote" signal.
func TestCRFlow_ReviewSubmittedAllClear_FromCoderabbit_GoesStaged(t *testing.T) {
	got := Decide(Input{
		Kind:                   EventKindReview,
		IssueStatus:            StatusCoderabbit,
		ReviewState:            ReviewApproved,
		ReviewByCR:             true,
		NoOpenCRChangesRequest: true,
		NoUnresolvedCRThreads:  true,
	})
	if got.Action != ActionSetStatusAndCloseAttempt || got.NewStatus != StatusStaged {
		t.Fatalf("first-pass clean review on coderabbit should jump to staged; got %+v", got)
	}
}

// 3. review_submitted with unresolved → resolving.
func TestCRFlow_ReviewSubmittedWithUnresolved_GoesResolving(t *testing.T) {
	cases := []struct {
		name        string
		reviewState ReviewState
	}{
		{"changes_requested → resolving", ReviewChangesRequested},
		{"commented + unresolved → resolving (soft)", ReviewCommented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(Input{
				Kind:                       EventKindReview,
				IssueStatus:                StatusCoderabbit,
				ReviewState:                tc.reviewState,
				ReviewByCR:                 true,
				LocalUnresolvedThreadCount: 4,
			})
			if got.Action != ActionSetStatusAndCloseAttempt || got.NewStatus != StatusResolving {
				t.Fatalf("%s on coderabbit should route to resolving; got %+v", tc.name, got)
			}
		})
	}
}

func TestCRFlow_v2_StreamThenWrappingChangesRequested(t *testing.T) {
	wrapping := Decide(Input{
		Kind:                       EventKindReview,
		IssueStatus:                StatusCoderabbit,
		ReviewState:                ReviewChangesRequested,
		ReviewByCR:                 true,
		LocalUnresolvedThreadCount: 2,
	})
	if wrapping.Action != ActionSetStatusAndCloseAttempt || wrapping.NewStatus != StatusResolving {
		t.Fatalf("v2 wrapping changes_requested should close attempt and route resolving; got %+v", wrapping)
	}
	if wrapping.AttemptOutcome != "completed_with_findings" || wrapping.AttemptReason != "changes_requested" {
		t.Fatalf("unexpected attempt closure: outcome=%q reason=%q", wrapping.AttemptOutcome, wrapping.AttemptReason)
	}
}

func TestCRFlow_v2_CommentedClean_RecordsPendingSettle(t *testing.T) {
	got := Decide(Input{
		Kind:                   EventKindReview,
		IssueStatus:            StatusCoderabbit,
		ReviewState:            ReviewCommented,
		ReviewByCR:             true,
		NoOpenCRChangesRequest: true,
		NoUnresolvedCRThreads:  true,
	})
	if got.Action != ActionRecordPendingApproval {
		t.Fatalf("v2 commented clean should record pending approval; got %+v", got)
	}
	if got.ActivityKind != "review_commented_clean_pending" || got.ReviewStateRecord != ReviewCommented || got.FindingsCount != 0 {
		t.Fatalf("unexpected pending approval decision: %+v", got)
	}
}

func TestCRFlow_InReviewCRChanges_IsNoop(t *testing.T) {
	got := Decide(Input{
		Kind:        EventKindReview,
		IssueStatus: StatusInReview,
		ReviewState: ReviewChangesRequested,
		ReviewByCR:  true,
	})
	if got.Action != ActionNoop {
		t.Fatalf("in_review + CR changes_requested should be silent under v2; got %+v", got)
	}
}

// G1: webhook redelivery on an already-staged issue must noop.
//
// CR's review.submitted (or thread events) can be redelivered by GitHub
// after the issue has already reached `staged`. A careless implementation
// could fire `staged → staged` (harmless no-op) or retrigger downstream
// activity rows. decideReview's promote branches gate on
// IssueStatus == coderabbit. Other statuses fall through to noop.
func TestCRFlow_Redelivery_AlreadyStaged_OnReview_IsNoop(t *testing.T) {
	cases := []struct {
		name        string
		reviewState ReviewState
	}{
		{"approved", ReviewApproved},
		{"commented (no unresolved)", ReviewCommented},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Decide(Input{
				Kind:                   EventKindReview,
				IssueStatus:            StatusStaged,
				ReviewState:            tc.reviewState,
				ReviewByCR:             true,
				NoOpenCRChangesRequest: true,
				NoUnresolvedCRThreads:  true,
			})
			if got.Action != ActionNoop {
				t.Fatalf("redelivered review on staged should be noop; got %+v", got)
			}
		})
	}
}

// G2: PR merged from coderabbit chains staged → done.
//
// When CR is not installed on the repo (or the merge races ahead of the
// CR predicate), pull_request.closed merged=true lands on `coderabbit`
// directly. The state machine emits NewStatus=staged with activity kind
// `pr_merged_from_coderabbit` so the audit trail shows the staged step.
// The webhook handler (applyDecision) then re-evaluates Decide on the
// refetched `staged` issue and applies the second leg → done.
//
// Both legs are pure-state-machine and tested separately above; this
// scenario test re-asserts them together so the chain is greppable.
func TestCRFlow_MergeFromCoderabbit_ChainsThroughStagedToDone(t *testing.T) {
	leg1 := Decide(Input{
		Kind:        EventKindPR,
		IssueStatus: StatusCoderabbit,
		PRAction:    PRActionClosed,
		Merged:      true,
	})
	if leg1.NewStatus != StatusStaged {
		t.Fatalf("leg 1 (coderabbit + merged): want staged; got %+v", leg1)
	}
	if leg1.ActivityKind != "pr_merged_from_coderabbit" {
		t.Fatalf("leg 1 activity kind: want pr_merged_from_coderabbit; got %q", leg1.ActivityKind)
	}

	leg2 := Decide(Input{
		Kind:        EventKindPR,
		IssueStatus: StatusStaged,
		PRAction:    PRActionClosed,
		Merged:      true,
	})
	if leg2.NewStatus != StatusDone {
		t.Fatalf("leg 2 (staged + merged): want done; got %+v", leg2)
	}
	if leg2.ActivityKind != "pr_merged" {
		t.Fatalf("leg 2 activity kind: want pr_merged; got %q", leg2.ActivityKind)
	}
}

// G1 corollary: redelivered review on `done` must also noop. Once a PR
// is merged, no CR event should re-flip the card. The state machine has
// no `done` branch in decide*, so it falls through to noop by default.
func TestCRFlow_Redelivery_AlreadyDone_IsNoop(t *testing.T) {
	got := Decide(Input{
		Kind:                   EventKindReview,
		IssueStatus:            StatusDone,
		ReviewState:            ReviewApproved,
		ReviewByCR:             true,
		NoOpenCRChangesRequest: true,
		NoUnresolvedCRThreads:  true,
	})
	if got.Action != ActionNoop {
		t.Fatalf("redelivered review on done should be noop; got %+v", got)
	}
}
