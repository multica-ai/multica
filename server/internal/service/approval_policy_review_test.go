package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/multica-ai/multica/server/internal/util"
)

// reviewEvents counts the policy sections of weekly reviews recorded for a workspace.
func reviewEvents(t *testing.T, e *policyEnv) int {
	t.Helper()
	var n int
	if err := sharedTestPool(t).QueryRow(context.Background(),
		`SELECT count(*) FROM governance_event WHERE workspace_id = $1 AND kind = 'review_published' AND details->>'review' = 'policy_reversals'`,
		e.wsID).Scan(&n); err != nil {
		t.Fatalf("count review events: %v", err)
	}
	return n
}

// TestWeeklyPolicyReviewListsReversalsOnce checks the weekly job's policy section:
// a reversed auto-approval of the week is listed once with its policy, a rerun
// writes nothing more, and a week with no reversal writes nothing.
func TestWeeklyPolicyReviewListsReversalsOnce(t *testing.T) {
	e := newPolicyEnv(t)
	ctx := context.Background()
	sq := e.squad("Weekly reviewed squad", AutonomyLevelTrusted)
	policy := e.createPolicy(decisionPolicy("Weekly reviewed"))
	res := e.file(sq, "", `{}`, false)
	if res.AutoDecision == nil {
		t.Fatal("setup: the filing was not auto-approved")
	}
	if err := e.policies.ReverseAutoApproval(ctx, e.ws, res.Proposal.ID, "The work was not needed."); err != nil {
		t.Fatalf("reverse: %v", err)
	}

	now := time.Now()
	n, err := e.policies.RecordWeeklyPolicyReview(ctx, e.ws, now)
	if err != nil || n != 1 {
		t.Fatalf("first weekly review: %d, %v; want 1 reversal listed", n, err)
	}
	if got := reviewEvents(t, e); got != 1 {
		t.Fatalf("review events after the first run = %d, want 1", got)
	}
	var details string
	if err := sharedTestPool(t).QueryRow(ctx,
		`SELECT details::text FROM governance_event WHERE workspace_id = $1 AND kind = 'review_published' AND details->>'review' = 'policy_reversals' LIMIT 1`,
		e.wsID).Scan(&details); err != nil {
		t.Fatalf("read review details: %v", err)
	}
	if policyID := util.UUIDToString(policy.ID); !strings.Contains(details, policyID) {
		t.Errorf("the review does not name the policy %s: %s", policyID, details)
	}

	if n, err := e.policies.RecordWeeklyPolicyReview(ctx, e.ws, now); err != nil || n != 0 {
		t.Fatalf("a rerun of the same week: %d, %v; want nothing written", n, err)
	}
	if got := reviewEvents(t, e); got != 1 {
		t.Fatalf("review events after a rerun = %d, want still 1", got)
	}

	// Five weeks on, the reversal is outside the week and nothing is written.
	later := now.Add(35 * 24 * time.Hour)
	if n, err := e.policies.RecordWeeklyPolicyReview(ctx, e.ws, later); err != nil || n != 0 {
		t.Fatalf("a week with no reversal: %d, %v; want nothing written", n, err)
	}
	if got := reviewEvents(t, e); got != 1 {
		t.Fatalf("review events for an empty week = %d, want still 1", got)
	}
}
